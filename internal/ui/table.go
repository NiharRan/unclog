// Package ui renders findings as tables and JSON.
package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/charmbracelet/lipgloss"
)

// WaitlistURL is shown in the scan footer.
// TODO: replace with the real waitlist link (open question in the spec).
const WaitlistURL = "https://github.com/NiharRan/unclog"

const maxLabel = 40

const barWidth = 16

type TableOptions struct {
	Color bool
	Quiet bool // hides the waitlist footer
	Now   time.Time
}

type styles struct{ bold, dim, safe, review, careful lipgloss.Style }

func newStyles(color bool) styles {
	plain := lipgloss.NewStyle()
	if !color {
		return styles{plain, plain, plain, plain, plain}
	}
	return styles{
		bold:    plain.Bold(true),
		dim:     plain.Foreground(lipgloss.Color("8")),
		safe:    plain.Foreground(lipgloss.Color("2")),
		review:  plain.Foreground(lipgloss.Color("3")),
		careful: plain.Foreground(lipgloss.Color("1")),
	}
}

func (s styles) risk(r engine.Risk) lipgloss.Style {
	switch r {
	case engine.RiskSafe:
		return s.safe
	case engine.RiskReview:
		return s.review
	}
	return s.careful
}

// RAMReport is the process side of a scan.
type RAMReport struct {
	Procs []engine.Proc
	Mem   engine.MemInfo
}

// Summary is the one shareable line printed after the tables.
func Summary(fs []engine.Finding, ram *RAMReport) string {
	var orphans int
	var rss uint64
	if ram != nil {
		orphans, rss = engine.OrphanTotals(ram.Procs)
	}
	switch {
	case len(fs) > 0 && orphans > 0:
		return fmt.Sprintf("Found %s reclaimable and %s RAM in %s.", engine.Total(fs), engine.Size(rss), plural(orphans, "orphaned process"))
	case len(fs) > 0:
		return fmt.Sprintf("Found %s reclaimable.", engine.Total(fs))
	case orphans > 0:
		return fmt.Sprintf("Found %s RAM in %s.", engine.Size(rss), plural(orphans, "orphaned process"))
	}
	return "No orphaned processes."
}

// RenderScan writes disk findings grouped by tool (largest group first), the RAM section
// when ram is set, then the summary line.
func RenderScan(w io.Writer, fs []engine.Finding, ram *RAMReport, o TableOptions) error {
	st := newStyles(o.Color)
	var b strings.Builder

	if len(fs) == 0 && (ram == nil || len(ram.Procs) == 0) {
		_, err := io.WriteString(w, "Nothing found.\n")
		return err
	}

	groups := map[string][]engine.Finding{}
	totals := map[string]engine.Size{}
	var tools []string
	labelW := 0
	for _, f := range fs {
		if _, ok := groups[f.Tool]; !ok {
			tools = append(tools, f.Tool)
		}
		groups[f.Tool] = append(groups[f.Tool], f)
		totals[f.Tool] += f.Size
		labelW = max(labelW, utf8.RuneCountInString(truncate(f.Label)))
	}
	sort.SliceStable(tools, func(i, j int) bool { return totals[tools[i]] > totals[tools[j]] })

	var grand engine.Size
	for _, t := range totals {
		grand += t
	}
	for _, tool := range tools {
		fmt.Fprintf(&b, "%s  %s  %s\n", st.bold.Render(tool), totals[tool], shareBar(uint64(totals[tool]), uint64(grand), st.safe))
		for _, f := range groups[tool] {
			fmt.Fprintf(&b, "  %s  %s  %s  %s  %s\n",
				pad(truncate(f.Label), labelW),
				st.dim.Render(pad(string(f.Category), 7)),
				st.risk(f.Risk).Render(pad(string(f.Risk), 7)),
				padLeft(f.Size.String(), 9),
				st.dim.Render(ago(f.LastUsed, o.Now)))
		}
		b.WriteString("\n")
	}

	if ram != nil && len(ram.Procs) > 0 {
		writeRAM(&b, ram, st)
	}

	b.WriteString(st.bold.Render(Summary(fs, ram)) + "\n")
	if !o.Quiet {
		fmt.Fprintf(&b, "%s\n", st.dim.Render("Want this automatic? Join the app waitlist → "+WaitlistURL))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeRAM(b *strings.Builder, ram *RAMReport, st styles) {
	var total uint64
	for _, p := range ram.Procs {
		total += p.RSS
	}
	fmt.Fprintf(b, "%s  %s across %s\n", st.bold.Render("RAM"), engine.Size(total), plural(len(ram.Procs), "process"))
	groups := engine.GroupByTool(ram.Procs)
	toolW := 0
	for _, g := range groups {
		toolW = max(toolW, utf8.RuneCountInString(g.Tool))
	}
	for _, g := range groups {
		orphan := ""
		if g.Orphans > 0 {
			orphan = st.careful.Render(fmt.Sprintf("%d orphaned (%s)", g.Orphans, engine.Size(g.OrphanRSS)))
		}
		row := fmt.Sprintf("  %s  %s  %s  %s", pad(g.Tool, toolW), padLeft(plural(g.Count, "process"), 12), padLeft(engine.Size(g.RSS).String(), 9), shareBar(g.RSS, total, st.review))
		if orphan != "" {
			row += "  " + orphan
		}
		b.WriteString(row + "\n")
	}
	if m := ram.Mem; m.Total > 0 {
		fmt.Fprintf(b, "  %s  %s\n", st.dim.Render(fmt.Sprintf("system: %s used of %s, pressure %s", engine.Size(m.Used), engine.Size(m.Total), m.Pressure)), pressureStyle(st, m.Used, m.Total).Render(Bar(percent(m.Used, m.Total), barWidth)))
	}
	b.WriteString("\n")
}

// shareBar is a bar plus a right-aligned percentage, e.g. "████░░░░  42%".
func shareBar(part, whole uint64, style lipgloss.Style) string {
	f := percent(part, whole)
	return style.Render(Bar(f, barWidth)) + " " + padLeft(fmt.Sprintf("%d%%", int(f*100+0.5)), 4)
}

// pressureStyle turns the system memory bar yellow above 70% and red above 90%.
func pressureStyle(st styles, used, total uint64) lipgloss.Style {
	switch f := percent(used, total); {
	case f > 0.9:
		return st.careful
	case f > 0.7:
		return st.review
	}
	return st.safe
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxLabel {
		return s
	}
	// Keep both ends: long paths and project names differ at the tail.
	r := []rune(s)
	return string(r[:maxLabel/2-1]) + "…" + string(r[len(r)-maxLabel/2:])
}

func pad(s string, w int) string {
	if n := w - utf8.RuneCountInString(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := w - utf8.RuneCountInString(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func ago(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return agoPlural(int(d.Hours()), "hour")
	case d < 60*24*time.Hour:
		return agoPlural(int(d.Hours()/24), "day")
	case d < 2*365*24*time.Hour:
		return agoPlural(int(d.Hours()/24/30), "month")
	}
	return agoPlural(int(d.Hours()/24/365), "year")
}

func agoPlural(n int, unit string) string { return plural(n, unit) + " ago" }

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	if strings.HasSuffix(unit, "s") {
		return fmt.Sprintf("%d %ses", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
