package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
	"github.com/NiharRan/unclog/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Overridable in tests.
var pickRestore = func(items []engine.Item) ([]engine.Item, bool, error) {
	m := ui.NewChecklist(items, false, ColorEnabled())
	m.Title, m.Verb = "Select what to restore from the Trash", "Restore"
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return nil, false, err
	}
	return m.Items(), m.Confirmed, nil
}

func newRestoreCmd() *cobra.Command {
	var all, dryRun bool
	c := &cobra.Command{
		Use:   "restore",
		Short: "Put files unclog moved to the Trash back where they were (macOS)",
		Long: `Lists what a past "clean" moved to the Trash and lets you pick what to put back.
Only items still in the Trash are listed, and nothing is overwritten.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			if env.OS != "darwin" {
				return errors.New("restore is only supported on macOS for now")
			}
			hist := safety.NewHistory(env)
			entries, err := engine.ListTrashed(hist.Path(), filepath.Join(env.Home, ".Trash"))
			if err != nil {
				return err
			}
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(out, "Nothing to restore.")
				return &exitError{code: ExitNothing}
			}
			items := make([]engine.Item, len(entries))
			for i, e := range entries {
				items[i] = engine.Item{Selected: all, Finding: engine.Finding{
					Tool: e.Time.Local().Format("2006-01-02"), Label: e.Orig, Path: e.Dest, Size: e.Size, Risk: engine.RiskSafe}}
			}
			if !all {
				if !isInteractive() {
					return errors.New("restore needs a terminal to pick items; use --all to restore everything")
				}
				var confirmed bool
				if items, confirmed, err = pickRestore(items); err != nil {
					return err
				}
				if !confirmed {
					_, _ = fmt.Fprintln(errOut, "Cancelled.")
					return &exitError{code: ExitCancelled}
				}
			}
			r := &engine.Restorer{Log: hist, Denied: func(p string) string {
				if bad, why := safety.IsDenied(p, env); bad {
					return why
				}
				if bad, why := safety.IsProtected(p, env); bad {
					return why
				}
				return ""
			}}
			failed := false
			for i, it := range items {
				if !it.Selected {
					continue
				}
				e := entries[i]
				if dryRun {
					_, _ = fmt.Fprintf(out, "would restore %s\n", e.Orig)
					continue
				}
				if err := r.Restore(e); err != nil {
					failed = true
					_, _ = fmt.Fprintf(errOut, "failed: %s: %v\n", e.Orig, err)
					continue
				}
				_, _ = fmt.Fprintf(out, "restored %s (%s)\n", e.Orig, e.Size)
			}
			if failed {
				return &exitError{code: ExitError}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "restore everything still in the Trash, no prompt")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be restored without changing anything")
	return c
}
