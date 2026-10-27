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

// Summary is the one shareable line printed after the table.
func Summary(fs []engine.Finding) string {
	return fmt.Sprintf("Found %s reclaimable.", engine.Total(fs))
}

// RenderScan writes findings grouped by tool (largest group first), then the summary line.
func RenderScan(w io.Writer, fs []engine.Finding, o TableOptions) error {
	st := newStyles(o.Color)
	var b strings.Builder

	if len(fs) == 0 {
		b.WriteString("Nothing found.\n")
		_, err := io.WriteString(w, b.String())
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

	for _, tool := range tools {
		fmt.Fprintf(&b, "%s  %s\n", st.bold.Render(tool), totals[tool])
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

	b.WriteString(st.bold.Render(Summary(fs)) + "\n")
	if !o.Quiet {
		fmt.Fprintf(&b, "%s\n", st.dim.Render("Want this automatic? Join the app waitlist → "+WaitlistURL))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxLabel {
		return s
	}
	return string([]rune(s)[:maxLabel-1]) + "…"
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
		return plural(int(d.Hours()), "hour")
	case d < 60*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 2*365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	}
	return plural(int(d.Hours()/24/365), "year")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
