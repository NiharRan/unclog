package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// ready runs the background in-use check and delivers its result, as bubbletea would.
func ready(m *Checklist) { m.Update(m.check()()) }

func press(m *Checklist, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func items() []engine.Item {
	run := engine.Finding{Tool: "Cursor", Label: "cache", Size: 5, Risk: engine.RiskSafe, ToolRunning: true}
	return []engine.Item{
		{Finding: engine.Finding{Tool: "pip", Label: "pip cache", Size: 10, Risk: engine.RiskSafe}, Selected: true},
		{Finding: engine.Finding{Tool: "HF", Label: "model", Size: 30, Risk: engine.RiskCareful}},
		{Finding: run},
	}
}

func selected(m *Checklist) []bool {
	var s []bool
	for _, it := range m.Items() {
		s = append(s, it.Selected)
	}
	return s
}

func eq(a, b []bool) bool {
	return len(a) == len(b) && (len(a) == 0 || a[0] == b[0] && eq(a[1:], b[1:]))
}

func TestChecklistToggleAndBulkKeys(t *testing.T) {
	m := NewChecklist(items(), false, false)
	press(m, "down", "space") // toggle the careful model on
	if !eq(selected(m), []bool{true, true, false}) {
		t.Errorf("after toggle: %v", selected(m))
	}
	press(m, "n")
	if !eq(selected(m), []bool{false, false, false}) {
		t.Errorf("after none: %v", selected(m))
	}
	press(m, "a")
	if !eq(selected(m), []bool{true, true, true}) { // everything at once
		t.Errorf("after select-all: %v", selected(m))
	}
	press(m, "s")
	if !eq(selected(m), []bool{true, false, false}) { // only safe and not running
		t.Errorf("after safe-only: %v", selected(m))
	}
	press(m, "up", "up", "up", "space") // cursor clamps at the top
	if !eq(selected(m), []bool{false, false, false}) {
		t.Errorf("after clamp+toggle: %v", selected(m))
	}
}

func TestChecklistCursorStaysInRange(t *testing.T) {
	m := NewChecklist(items(), false, false)
	press(m, "j", "j", "j", "j", "j")
	if m.cursor != 2 {
		t.Errorf("cursor = %d; want 2", m.cursor)
	}
}

func TestChecklistConfirmFlow(t *testing.T) {
	m := NewChecklist(items(), false, false)
	ready(m)
	press(m, "enter")
	if m.stage != stageConfirm || !strings.Contains(m.View(), "Move to the Trash") {
		t.Fatalf("confirm view:\n%s", m.View())
	}
	press(m, "x") // any other key goes back
	if m.stage != stagePick || m.Confirmed {
		t.Error("a non-y key must go back")
	}
	press(m, "enter", "y")
	if !m.Confirmed || m.Cancelled {
		t.Error("y should confirm")
	}
}

func TestChecklistEnterWithNothingSelectedStays(t *testing.T) {
	m := NewChecklist(items(), false, false)
	press(m, "n", "enter")
	if m.stage != stagePick {
		t.Error("cannot continue with nothing selected")
	}
}

func TestChecklistConfirmWarnsAboutRisk(t *testing.T) {
	m := NewChecklist(items(), false, false)
	ready(m)
	press(m, "down", "space", "enter") // careful item on, nothing blocked
	if v := m.View(); !strings.Contains(v, "marked review or careful") || !strings.Contains(v, "2 items") {
		t.Errorf("view:\n%s", v)
	}
}

func TestChecklistBlockedPopup(t *testing.T) {
	m := NewChecklist(items(), false, false)
	ready(m)
	press(m, "down", "down", "space", "enter") // the running-tool item joins the selection
	if m.stage != stageBlocked || !strings.Contains(m.View(), "Cursor is running") || !strings.Contains(m.View(), "can't be removed") {
		t.Fatalf("expected popup, stage=%v:\n%s", m.stage, m.View())
	}
	press(m, "esc")
	if m.stage != stagePick {
		t.Error("esc must go back to the list")
	}
	press(m, "enter", "s") // skip: the blocked item is deselected
	if m.stage != stageConfirm || !eq(selected(m), []bool{true, false, false}) {
		t.Errorf("skip: stage=%v selected=%v", m.stage, selected(m))
	}
	press(m, "x", "down", "down", "space", "enter", "t") // try anyway keeps it
	if m.stage != stageConfirm || !eq(selected(m), []bool{true, false, true}) {
		t.Errorf("try anyway: stage=%v selected=%v", m.stage, selected(m))
	}
}

func TestChecklistBlockedHook(t *testing.T) {
	m := NewChecklist(items(), false, false)
	m.Blocked = func(it engine.Item) string {
		if it.Label == "pip cache" {
			return "no permission to remove it"
		}
		return ""
	}
	ready(m)
	press(m, "enter")
	if m.stage != stageBlocked || !strings.Contains(m.View(), "no permission to remove it") {
		t.Errorf("stage=%v:\n%s", m.stage, m.View())
	}
	press(m, "s") // skipping the only selected item leaves nothing: back to the list
	if m.stage != stagePick {
		t.Errorf("stage=%v, want pick", m.stage)
	}
}

func TestChecklistCancel(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := NewChecklist(items(), false, false)
		press(m, k)
		if !m.Cancelled || m.Confirmed {
			t.Errorf("%s did not cancel", k)
		}
	}
	m := NewChecklist(items(), false, false)
	press(m, "enter", "ctrl+c")
	if !m.Cancelled {
		t.Error("ctrl+c must cancel from the confirm screen")
	}
}

func TestChecklistPermanentNeedsTypedWord(t *testing.T) {
	m := NewChecklist(items(), true, false)
	ready(m)
	press(m, "enter")
	if !strings.Contains(m.View(), "Delete permanently") || !strings.Contains(m.View(), "cannot be undone") {
		t.Fatalf("view:\n%s", m.View())
	}
	press(m, "y", "enter") // wrong word: back to the list
	if m.Confirmed || m.stage != stagePick {
		t.Fatal("y must not confirm a permanent delete")
	}
	press(m, "enter", "d", "e", "l", "e", "t", "x", "backspace", "e", "enter")
	if !m.Confirmed {
		t.Errorf("typing the word should confirm (typed=%q)", m.typed)
	}
}

func TestChecklistScrollsWithCursor(t *testing.T) {
	var many []engine.Item
	for i := 0; i < 40; i++ {
		many = append(many, engine.Item{Finding: engine.Finding{Tool: "T", Label: "item", Size: 1}})
	}
	m := NewChecklist(many, false, false)
	m.Update(tea.WindowSizeMsg{Height: 13}) // 5 visible rows
	for i := 0; i < 20; i++ {
		press(m, "j")
	}
	if m.cursor != 20 || m.cursor < m.offset || m.cursor >= m.offset+m.height {
		t.Errorf("cursor=%d offset=%d height=%d", m.cursor, m.offset, m.height)
	}
	if !strings.Contains(m.View(), "showing") {
		t.Error("long lists should say how many rows are shown")
	}
}

func TestChecklistViewMarksRunningTools(t *testing.T) {
	v := NewChecklist(items(), false, false).View()
	if !strings.Contains(v, "Cursor is running") || !strings.Contains(v, "[✔]") || !strings.Contains(v, "[ ]") {
		t.Errorf("view:\n%s", v)
	}
}

func TestRenderResults(t *testing.T) {
	res := []engine.Result{
		{Finding: engine.Finding{Tool: "pip", Label: "pip cache", Size: 10 << 20}, Action: "trash", Outcome: engine.Trashed},
		{Finding: engine.Finding{Tool: "HF", Label: "model", Size: 1}, Action: "trash", Outcome: engine.Refused, Err: errors.New("in use")},
		{Finding: engine.Finding{Tool: "X", Label: "bad", Size: 1}, Action: "trash", Outcome: engine.Failed, Err: errors.New("boom")},
	}
	var b bytes.Buffer
	if err := RenderResults(&b, res, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"trashed", "pip: pip cache", "skipped HF: model", "in use", "failed  X: bad", "boom", "Moved 1 item (10.0 MB) to the Trash.", "Empty the Trash", "1 item skipped by safety checks.", "1 item failed."} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
	b.Reset()
	_ = RenderPlan(&b, []engine.Result{{Finding: res[0].Finding, Action: "trash", Outcome: engine.DryRun}}, false)
	if !strings.Contains(b.String(), "would trash") || !strings.Contains(b.String(), "Nothing was changed") {
		t.Errorf("plan:\n%s", b.String())
	}
	b.Reset()
	_ = RenderResults(&b, []engine.Result{{Finding: res[0].Finding, Action: "delete", Outcome: engine.Deleted}}, false)
	if !strings.Contains(b.String(), "Cleaned 1 item, freed 10.0 MB.") {
		t.Errorf("permanent summary:\n%s", b.String())
	}
}

func TestChecklistCustomWording(t *testing.T) {
	m := NewChecklist(items(), false, false)
	ready(m)
	m.Title, m.Verb = "Select processes to stop", "Terminate"
	if !strings.Contains(m.View(), "Select processes to stop") {
		t.Errorf("title missing:\n%s", m.View())
	}
	press(m, "enter")
	if !strings.Contains(m.View(), "Terminate") || strings.Contains(m.View(), "Trash") {
		t.Errorf("confirm view:\n%s", m.View())
	}
}

func TestRenderKill(t *testing.T) {
	p := func(pid int32) engine.Proc {
		return engine.Proc{PID: pid, Name: "node", Tool: "MCP servers", RSS: 100 << 20}
	}
	res := []engine.KillResult{
		{Proc: p(1000), Outcome: engine.Terminated},
		{Proc: p(1001), Outcome: engine.Killed},
		{Proc: p(50), Outcome: engine.Refused, Err: errors.New("pid 50 is below 100")},
		{Proc: p(1002), Outcome: engine.Failed, Err: errors.New("SIGTERM: denied")},
	}
	var b bytes.Buffer
	if err := RenderKill(&b, res, false, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stopped", "MCP servers: node (pid 1000)", "killed", "skipped MCP servers: node (pid 50)", "below 100", "failed  MCP servers: node (pid 1002)",
		"Stopped 2 processes, freed about 200.0 MB of RAM.", "1 process skipped by safety checks.", "1 process failed."} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
	b.Reset()
	_ = RenderKill(&b, []engine.KillResult{{Proc: p(1000), Outcome: engine.DryRun}}, true, false)
	if !strings.Contains(b.String(), "would stop") || !strings.Contains(b.String(), "Dry run: 1 process, about 100.0 MB of RAM. Nothing was changed.") {
		t.Errorf("dry:\n%s", b.String())
	}
}

func TestChecklistEnterWaitsForSlowCheck(t *testing.T) {
	m := NewChecklist(items(), false, false)
	press(m, "enter") // check not finished yet
	if m.stage != stageChecking || !strings.Contains(m.View(), "Checking") {
		t.Fatalf("stage=%v:\n%s", m.stage, m.View())
	}
	ready(m) // result arrives: moves on by itself
	if m.stage != stageConfirm {
		t.Errorf("stage=%v, want confirm", m.stage)
	}
}

func TestProgressViewShowsBarAndRecentItems(t *testing.T) {
	its := []engine.Item{
		{Finding: engine.Finding{Tool: "pip", Label: "pip cache", Size: 10 << 20}, Selected: true},
		{Finding: engine.Finding{Tool: "HF", Label: "model", Size: 30 << 20}, Selected: true},
	}
	m := &Progress{title: "Moving to the Trash", total: 2, all: selectedSize(its), st: newStyles(false)}
	m.Update(startMsg{its[0].Finding})
	m.Update(resultMsg{1, engine.Result{Finding: its[0].Finding, Outcome: engine.Trashed}})
	m.Update(startMsg{its[1].Finding})
	v := m.View()
	for _, want := range []string{"Moving to the Trash", "1/2", "50%", "10.0 MB of 40.0 MB", "now HF: model", "✔ pip: pip cache"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in\n%s", want, v)
		}
	}
	m.Update(resultMsg{2, engine.Result{Finding: its[1].Finding, Outcome: engine.Refused}})
	if v := m.View(); !strings.Contains(v, "! HF: model") || !strings.Contains(v, "10.0 MB of 40.0 MB") {
		t.Errorf("skipped items must not count as freed:\n%s", v)
	}
}
