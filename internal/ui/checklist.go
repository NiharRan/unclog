package ui

import (
	"fmt"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type stage int

const (
	stagePick    stage = iota
	stageBlocked       // popup: some selected items cannot be removed right now
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

	// Blocked, if set, returns why an item cannot be removed right now ("" when it can).
	// Selected blocked items trigger a popup before the confirm page.
	Blocked func(engine.Item) string

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
		switch m.stage {
		case stageBlocked:
			return m.updateBlocked(msg)
		case stageConfirm:
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
	case "a": // everything at once
		for i := range m.items {
			m.items[i].Selected = true
		}
	case "s": // only what is safe and not in use: the default selection
		for i := range m.items {
			m.items[i].Selected = m.items[i].Risk == engine.RiskSafe && !m.items[i].ToolRunning
		}
	case "n":
		for i := range m.items {
			m.items[i].Selected = false
		}
	case "enter":
		switch {
		case len(engine.Selected(m.items)) == 0:
		case len(m.blockedSelected()) > 0:
			m.stage = stageBlocked
		default:
			m.stage = stageConfirm
		}
	case "q", "esc":
		m.Cancelled = true
		return m, tea.Quit
	}
	m.scroll()
	return m, nil
}

type blockedItem struct {
	idx    int
	reason string
}

func (m *Checklist) blockedSelected() []blockedItem {
	var out []blockedItem
	for i, it := range m.items {
		if !it.Selected {
			continue
		}
		switch {
		case m.Blocked != nil && m.Blocked(it) != "":
			out = append(out, blockedItem{i, m.Blocked(it)})
		case it.ToolRunning:
			out = append(out, blockedItem{i, it.Tool + " is running"})
		}
	}
	return out
}

// updateBlocked handles the popup: s skips the blocked items, t tries them anyway, esc goes back.
func (m *Checklist) updateBlocked(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s", "S":
		for _, b := range m.blockedSelected() {
			m.items[b.idx].Selected = false
		}
		if len(engine.Selected(m.items)) == 0 {
			m.stage = stagePick
		} else {
			m.stage = stageConfirm
		}
	case "t", "T":
		m.stage = stageConfirm
	case "esc", "q", "n":
		m.stage = stagePick
	}
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
	switch m.stage {
	case stageBlocked:
		return m.blockedView()
	case stageConfirm:
		return m.confirmView()
	}
	var b strings.Builder
	title := m.Title
	if title == "" {
		title = "Select what to clean"
	}
	b.WriteString(m.st.bold.Render(title) + "\n")
	b.WriteString(m.st.dim.Render("Move with ↑ ↓, tick items with Space, then press Enter to review. Nothing is removed until you confirm.") + "\n\n")
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
			cursor = m.st.bold.Render("▸ ")
		}
		if it.Selected {
			box = m.st.safe.Render("[✔]")
		}
		note := ""
		if it.ToolRunning {
			note = m.st.review.Render("  " + it.Tool + " is running")
		}
		row := fmt.Sprintf("%s %s  %s  %s", box, pad(truncate(it.Label), maxLabel),
			m.st.risk(it.Risk).Render(pad(string(it.Risk), 7)), padLeft(it.Size.String(), 9))
		if i == m.cursor {
			row = m.st.bold.Render(row)
		}
		b.WriteString(cursor + row + note + "\n")
	}
	if len(m.items) > end-m.offset {
		fmt.Fprintf(&b, "%s\n", m.st.dim.Render(fmt.Sprintf("  … showing %d–%d of %d, keep scrolling", m.offset+1, end, len(m.items))))
	}
	sel := engine.Selected(m.items)
	fmt.Fprintf(&b, "\n%s  %s\n", m.st.bold.Render(fmt.Sprintf("Selected: %d of %d items, %s", len(sel), len(m.items), selectedSize(sel))),
		m.st.safe.Render(Bar(percent(uint64(selectedSize(sel)), uint64(selectedSize(m.items))), barWidth)))
	b.WriteString(m.st.dim.Render("safe: comes back on its own · review: check first · careful: may be hard to get back") + "\n")
	b.WriteString(m.st.dim.Render("Space tick · a select all · s safe only · n none · Enter review · q quit") + "\n")
	return b.String()
}

func selectedSize(items []engine.Item) engine.Size {
	var s engine.Size
	for _, it := range items {
		s += it.Size
	}
	return s
}

// box draws a titled panel around body.
func (m *Checklist) box(title, body string, border lipgloss.TerminalColor) string {
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2)
	if m.st.color {
		st = st.BorderForeground(border)
	}
	return st.Render(m.st.bold.Render(title)+"\n\n"+body) + "\n"
}

func (m *Checklist) blockedView() string {
	blocked := m.blockedSelected()
	var b strings.Builder
	fmt.Fprintf(&b, "%s cannot be removed right now:\n\n", plural(len(blocked), "selected item"))
	for i, bl := range blocked {
		if i == 5 {
			fmt.Fprintf(&b, "  %s\n", m.st.dim.Render(fmt.Sprintf("… and %d more", len(blocked)-5)))
			break
		}
		it := m.items[bl.idx]
		fmt.Fprintf(&b, "  • %s: %s  %s\n", it.Tool, truncate(it.Label), m.st.review.Render(bl.reason))
	}
	b.WriteString("\nIf you try anyway they may fail or be skipped.\n\n")
	b.WriteString(m.st.bold.Render("[S]") + " Skip these and continue    " + m.st.bold.Render("[T]") + " Try anyway    " + m.st.bold.Render("[Esc]") + " Go back")
	return m.box("Some items can't be removed", b.String(), lipgloss.Color("3"))
}

func (m *Checklist) confirmView() string {
	sel := engine.Selected(m.items)
	var risky int
	perTool := map[string]engine.Size{}
	counts := map[string]int{}
	var tools []string
	for _, it := range sel {
		if it.Risk != engine.RiskSafe {
			risky++
		}
		if _, ok := counts[it.Tool]; !ok {
			tools = append(tools, it.Tool)
		}
		counts[it.Tool]++
		perTool[it.Tool] += it.Size
	}
	verb, mode := m.Verb, ""
	switch {
	case verb != "":
	case m.permanent:
		verb, mode = "Delete permanently", m.st.careful.Render("Permanent. This cannot be undone.")
	default:
		verb, mode = "Move to the Trash", m.st.safe.Render("Trash. Items can be taken back out.")
	}
	toolW := 0
	for _, t := range tools {
		toolW = max(toolW, len([]rune(t)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", m.st.dim.Render(pad("Action", 8)), verb)
	fmt.Fprintf(&b, "%s  %s\n", m.st.dim.Render(pad("Items", 8)), plural(len(sel), "item"))
	fmt.Fprintf(&b, "%s  %s\n", m.st.dim.Render(pad("Size", 8)), selectedSize(sel))
	if mode != "" {
		fmt.Fprintf(&b, "%s  %s\n", m.st.dim.Render(pad("Mode", 8)), mode)
	}
	b.WriteString("\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "  %s  %s  %s\n", pad(t, toolW), padLeft(plural(counts[t], "item"), 8), padLeft(perTool[t].String(), 9))
	}
	if risky > 0 {
		fmt.Fprintf(&b, "\n%s\n", m.st.review.Render(plural(risky, "selected item")+" marked review or careful."))
	}
	b.WriteString("\n")
	border := lipgloss.Color("2")
	if m.permanent {
		border = lipgloss.Color("1")
		fmt.Fprintf(&b, "Type %s and press Enter to confirm, Esc to go back: %s", m.st.bold.Render(confirmWord), m.typed+"▏")
	} else {
		b.WriteString(m.st.bold.Render("[Y]") + " Confirm    " + m.st.bold.Render("[any other key]") + " Go back")
	}
	return m.box("Review and confirm", b.String(), border)
}
