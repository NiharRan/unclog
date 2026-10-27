package ui

import (
	"fmt"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

type stage int

const (
	stagePick stage = iota
	stageConfirm
)

// confirmWord must be typed to confirm a permanent delete.
const confirmWord = "delete"

// Checklist is the bubbletea model for `clean`: pick items, then confirm.
// Safe items start checked; review and careful items are listed unchecked.
type Checklist struct {
	items     []engine.Item
	cursor    int
	offset    int
	height    int
	stage     stage
	typed     string
	permanent bool
	st        styles

	// Title and Verb override the clean wording, e.g. "Select processes" and "Terminate".
	Title, Verb string

	Confirmed bool
	Cancelled bool
}

func NewChecklist(items []engine.Item, permanent, color bool) *Checklist {
	return &Checklist{items: items, height: 15, permanent: permanent, st: newStyles(color)}
}

// Items returns the items with the user's final selection.
func (m *Checklist) Items() []engine.Item { return m.items }

func (m *Checklist) Init() tea.Cmd { return nil }

func (m *Checklist) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(5, msg.Height-8)
		m.scroll()
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.Cancelled = true
			return m, tea.Quit
		}
		if m.stage == stageConfirm {
			return m.updateConfirm(msg)
		}
		return m.updatePick(msg)
	}
	return m, nil
}

func (m *Checklist) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.items)-1, m.cursor+1)
	case " ", "x":
		if len(m.items) > 0 {
			m.items[m.cursor].Selected = !m.items[m.cursor].Selected
		}
	case "a": // all safe, tools not running
		for i := range m.items {
			m.items[i].Selected = m.items[i].Risk == engine.RiskSafe && !m.items[i].ToolRunning
		}
	case "n":
		for i := range m.items {
			m.items[i].Selected = false
		}
	case "enter":
		if len(engine.Selected(m.items)) > 0 {
			m.stage = stageConfirm
		}
	case "q", "esc":
		m.Cancelled = true
		return m, tea.Quit
	}
	m.scroll()
	return m, nil
}

func (m *Checklist) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.permanent {
		switch msg.Type {
		case tea.KeyEnter:
			if m.typed == confirmWord {
				m.Confirmed = true
				return m, tea.Quit
			}
			m.stage, m.typed = stagePick, ""
		case tea.KeyEsc:
			m.stage, m.typed = stagePick, ""
		case tea.KeyBackspace:
			if m.typed != "" {
				m.typed = m.typed[:len(m.typed)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			m.typed += string(msg.Runes)
		}
		return m, nil
	}
	if msg.String() == "y" || msg.String() == "Y" {
		m.Confirmed = true
		return m, tea.Quit
	}
	m.stage = stagePick
	return m, nil
}

func (m *Checklist) scroll() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
}

func (m *Checklist) View() string {
	if m.Confirmed || m.Cancelled {
		return ""
	}
	if m.stage == stageConfirm {
		return m.confirmView()
	}
	var b strings.Builder
	title := m.Title
	if title == "" {
		title = "Select what to clean"
	}
	b.WriteString(m.st.bold.Render(title) + "\n\n")
	end := min(len(m.items), m.offset+m.height)
	lastTool := ""
	for i := m.offset; i < end; i++ {
		it := m.items[i]
		if it.Tool != lastTool {
			b.WriteString(m.st.bold.Render(it.Tool) + "\n")
			lastTool = it.Tool
		}
		cursor, box := "  ", "[ ]"
		if i == m.cursor {
			cursor = "> "
		}
		if it.Selected {
			box = "[x]"
		}
		note := ""
		if it.ToolRunning {
			note = m.st.review.Render("  " + it.Tool + " is running")
		}
		fmt.Fprintf(&b, "%s%s %s  %s  %s%s\n", cursor, box, pad(truncate(it.Label), maxLabel),
			m.st.risk(it.Risk).Render(pad(string(it.Risk), 7)), padLeft(it.Size.String(), 9), note)
	}
	if len(m.items) > end-m.offset {
		fmt.Fprintf(&b, "%s\n", m.st.dim.Render(fmt.Sprintf("  … %d of %d shown", end-m.offset, len(m.items))))
	}
	sel := engine.Selected(m.items)
	fmt.Fprintf(&b, "\n%s\n", m.st.bold.Render(fmt.Sprintf("Selected: %d items, %s", len(sel), selectedSize(sel))))
	b.WriteString(m.st.dim.Render("space toggle · a all safe · n none · enter continue · q quit") + "\n")
	return b.String()
}

func selectedSize(items []engine.Item) engine.Size {
	var s engine.Size
	for _, it := range items {
		s += it.Size
	}
	return s
}

func (m *Checklist) confirmView() string {
	sel := engine.Selected(m.items)
	var risky, running int
	for _, it := range sel {
		if it.Risk != engine.RiskSafe {
			risky++
		}
		if it.ToolRunning {
			running++
		}
	}
	var b strings.Builder
	verb := m.Verb
	switch {
	case verb != "":
	case m.permanent:
		verb = "PERMANENTLY DELETE"
	default:
		verb = "Move to the Trash"
	}
	fmt.Fprintf(&b, "%s\n\n", m.st.bold.Render(fmt.Sprintf("%s: %d items, %s", verb, len(sel), selectedSize(sel))))
	if risky > 0 {
		fmt.Fprintf(&b, "%s\n", m.st.review.Render(fmt.Sprintf("%d selected items are marked review or careful.", risky)))
	}
	if running > 0 {
		fmt.Fprintf(&b, "%s\n", m.st.review.Render(fmt.Sprintf("%d selected items belong to a tool that is running.", running)))
	}
	if m.permanent {
		fmt.Fprintf(&b, "%s\n", m.st.careful.Render("This cannot be undone."))
		fmt.Fprintf(&b, "Type %q and press enter to confirm, esc to go back: %s\n", confirmWord, m.typed)
	} else {
		b.WriteString("Press y to confirm, any other key to go back.\n")
	}
	return b.String()
}
