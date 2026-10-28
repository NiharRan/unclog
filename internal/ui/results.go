package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
)

// RenderPlan lists what a dry run would do, using the same results a real run produces.
func RenderPlan(w io.Writer, results []engine.Result, color bool) error {
	return renderResults(w, results, color, true)
}

// RenderResults prints one line per item and a closing summary.
func RenderResults(w io.Writer, results []engine.Result, color bool) error {
	return renderResults(w, results, color, false)
}

func renderResults(w io.Writer, results []engine.Result, color, dry bool) error {
	st := newStyles(color)
	var b strings.Builder
	var n, refused, failed int
	var total engine.Size
	trashed := false
	for _, r := range results {
		f := r.Finding
		label := fmt.Sprintf("%s: %s", f.Tool, truncate(f.Label))
		switch r.Outcome {
		case engine.Refused:
			refused++
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.review.Render(symWarn), st.review.Render("skipped"), label, st.dim.Render(r.Err.Error()))
		case engine.Failed:
			failed++
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.careful.Render(symFail), st.careful.Render("failed "), label, r.Err)
		default:
			n++
			total += f.Size
			trashed = trashed || r.Outcome == engine.Trashed
			word := map[engine.Outcome]string{engine.Trashed: "trashed", engine.Deleted: "deleted", engine.Ran: "cleaned", engine.DryRun: "would " + r.Action}[r.Outcome]
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.safe.Render(symOK), st.safe.Render(pad(word, 7)), label, st.dim.Render(f.Size.String()))
			if r.Err != nil {
				fmt.Fprintf(&b, "          %s\n", st.review.Render(r.Err.Error()))
			}
		}
	}
	b.WriteString("\n")
	switch {
	case dry:
		fmt.Fprintf(&b, "%s\n", st.bold.Render(fmt.Sprintf("Dry run: %s, %s. Nothing was changed.", plural(n, "item"), total)))
	case trashed:
		fmt.Fprintf(&b, "%s\n%s\n", st.bold.Render(fmt.Sprintf("Moved %s (%s) to the Trash.", plural(n, "item"), total)),
			st.dim.Render("Empty the Trash to reclaim the disk space."))
	default:
		fmt.Fprintf(&b, "%s\n", st.bold.Render(fmt.Sprintf("Cleaned %s, freed %s.", plural(n, "item"), total)))
	}
	if refused > 0 {
		fmt.Fprintf(&b, "%s\n", plural(refused, "item")+" skipped by safety checks.")
	}
	if failed > 0 {
		fmt.Fprintf(&b, "%s\n", st.careful.Render(plural(failed, "item")+" failed."))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// RenderKill prints one line per process and a closing summary. dry means nothing was signalled.
func RenderKill(w io.Writer, results []engine.KillResult, dry, color bool) error {
	st := newStyles(color)
	var b strings.Builder
	var n, refused, failed int
	var rss uint64
	for _, r := range results {
		p := r.Proc
		label := engine.ProcLabel(p)
		if p.Tool != "" {
			label = p.Tool + ": " + label
		}
		switch r.Outcome {
		case engine.Refused:
			refused++
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.review.Render(symWarn), st.review.Render("skipped"), label, st.dim.Render(r.Err.Error()))
		case engine.Failed:
			failed++
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.careful.Render(symFail), st.careful.Render("failed "), label, r.Err)
		default:
			n++
			rss += p.RSS
			word := map[engine.Outcome]string{engine.Terminated: "stopped", engine.Killed: "killed", engine.DryRun: "would stop"}[r.Outcome]
			fmt.Fprintf(&b, "%s %s %s  %s\n", st.safe.Render(symOK), st.safe.Render(pad(word, 7)), label, st.dim.Render(engine.Size(p.RSS).String()))
		}
	}
	b.WriteString("\n")
	if dry {
		fmt.Fprintf(&b, "%s\n", st.bold.Render(fmt.Sprintf("Dry run: %s, about %s of RAM. Nothing was changed.", plural(n, "process"), engine.Size(rss))))
	} else if n == 0 {
		b.WriteString(st.bold.Render("Nothing was stopped.") + "\n")
	} else {
		fmt.Fprintf(&b, "%s\n", st.bold.Render(fmt.Sprintf("Stopped %s, freed about %s of RAM.", plural(n, "process"), engine.Size(engine.FreedRAM(results)))))
	}
	if refused > 0 {
		fmt.Fprintf(&b, "%s\n", plural(refused, "process")+" skipped by safety checks.")
	}
	if failed > 0 {
		fmt.Fprintf(&b, "%s\n", st.careful.Render(plural(failed, "process")+" failed."))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
