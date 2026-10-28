package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	progressBarWidth = 30
	recentRows       = 8
)

type (
	startMsg  struct{ f engine.Finding }
	resultMsg struct {
		done int
		r    engine.Result
	}
	doneMsg struct{ results []engine.Result }
)

// Progress is the live view shown while items are removed one by one.
type Progress struct {
	title   string
	total   int
	done    int
	current string
	recent  []engine.Result
	freed   engine.Size
	all     engine.Size
	st      styles
	cancel  context.CancelFunc

	results  []engine.Result
	finished bool
}

func (m *Progress) Init() tea.Cmd { return nil }

func (m *Progress) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case startMsg:
		m.current = fmt.Sprintf("%s: %s", msg.f.Tool, truncate(msg.f.Label))
	case resultMsg:
		m.done = msg.done
		m.recent = append(m.recent, msg.r)
		if len(m.recent) > recentRows {
			m.recent = m.recent[1:]
		}
		if msg.r.Outcome != engine.Refused && msg.r.Outcome != engine.Failed {
			m.freed += msg.r.Finding.Size
		}
	case doneMsg:
		m.results, m.finished = msg.results, true
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC && m.cancel != nil {
			m.cancel() // stops after the item in progress; finished items stay finished
			m.current = "Stopping after this item…"
		}
	}
	return m, nil
}

func (m *Progress) View() string {
	if m.finished {
		return ""
	}
	var b strings.Builder
	f := float64(m.done) / float64(max(m.total, 1))
	fmt.Fprintf(&b, "%s\n\n", m.st.bold.Render(m.title))
	fmt.Fprintf(&b, "  %s  %d/%d  %s   %s\n", m.st.safe.Render(Bar(f, progressBarWidth)), m.done, m.total,
		padLeft(fmt.Sprintf("%d%%", int(f*100+0.5)), 4), m.st.dim.Render(fmt.Sprintf("%s of %s", m.freed, m.all)))
	if m.current != "" {
		fmt.Fprintf(&b, "  %s %s\n", m.st.dim.Render("now"), m.current)
	}
	b.WriteString("\n")
	for _, r := range m.recent {
		label := fmt.Sprintf("%s: %s", r.Finding.Tool, truncate(r.Finding.Label))
		switch r.Outcome {
		case engine.Refused:
			fmt.Fprintf(&b, "  %s %s  %s\n", m.st.review.Render(symWarn), label, m.st.dim.Render("skipped"))
		case engine.Failed:
			fmt.Fprintf(&b, "  %s %s  %s\n", m.st.careful.Render(symFail), label, m.st.dim.Render("failed"))
		default:
			fmt.Fprintf(&b, "  %s %s  %s\n", m.st.safe.Render(symOK), label, m.st.dim.Render(r.Finding.Size.String()))
		}
	}
	b.WriteString("\n" + m.st.dim.Render("Ctrl+C stops after the current item.") + "\n")
	return b.String()
}

// RunProgress runs exec in the background and shows its progress until it returns.
// exec must call the hooks it is given (pass them in ExecOptions). Ctrl+C cancels ctx.
func RunProgress(ctx context.Context, title string, items []engine.Item, color bool,
	exec func(ctx context.Context, onStart func(int, int, engine.Finding), onResult func(int, int, engine.Result)) []engine.Result,
) ([]engine.Result, error) {
	sel := engine.Selected(items)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &Progress{title: title, total: len(sel), all: selectedSize(sel), st: newStyles(color), cancel: cancel}
	p := tea.NewProgram(m)
	go func() {
		res := exec(ctx,
			func(_, _ int, f engine.Finding) { p.Send(startMsg{f}) },
			func(done, _ int, r engine.Result) { p.Send(resultMsg{done, r}) })
		p.Send(doneMsg{res})
	}()
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return m.results, nil
}
