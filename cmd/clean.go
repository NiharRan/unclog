package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
	"github.com/NiharRan/unclog/internal/trash"
	"github.com/NiharRan/unclog/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Overridable in tests.
var (
	newCleaner = func(env engine.Env) *engine.Cleaner {
		return &engine.Cleaner{Guard: safety.NewChecker(env), Log: safety.NewHistory(env), Trash: trash.Move, Exec: engine.DefaultExec}
	}
	isInteractive = func() bool { return isTerminal(os.Stdin) && isTerminal(os.Stdout) }
	runChecklist  = func(items []engine.Item, permanent bool) ([]engine.Item, bool, error) {
		m := ui.NewChecklist(items, permanent, ColorEnabled())
		m.Blocked = blockedReason()
		if _, err := tea.NewProgram(m).Run(); err != nil {
			return nil, false, err
		}
		return m.Items(), m.Confirmed, nil
	}
)

// blockedReason explains why an item cannot be removed right now, or "" if it can.
// shortcut: write permission is judged from the parent folder's owner bit, not a real access check.
func blockedReason() func(engine.Item) string {
	var chk *safety.Checker
	if env, err := newEnv(); err == nil {
		chk = safety.NewChecker(env)
	}
	return func(it engine.Item) string {
		if it.Path == "" {
			return ""
		}
		if fi, err := os.Stat(filepath.Dir(it.Path)); err == nil && fi.Mode().Perm()&0o200 == 0 {
			return "no permission to remove it"
		}
		if chk != nil && chk.IsOpen(it.Path) {
			return "in use by a running program"
		}
		return ""
	}
}

func newCleanCmd() *cobra.Command {
	var dryRun, yes, permanent bool
	var categories []string
	var olderThan string
	c := &cobra.Command{
		Use:   "clean",
		Short: "Pick findings and move them to the Trash",
		Long: `Scans, lists what it found and lets you pick. Only safe items are pre-selected;
review and careful items are listed unchecked. Files go to the OS Trash unless --permanent.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cats, err := engine.ParseCategories(categories)
			if err != nil {
				return fmt.Errorf("--category: %w", err)
			}
			var age time.Duration
			if olderThan != "" {
				if age, err = engine.ParseAge(olderThan); err != nil {
					return fmt.Errorf("--older-than: %w", err)
				}
			}
			env, err := newEnv()
			if err != nil {
				return err
			}
			cfg, err := loadConfig(cmd, env)
			if err != nil {
				return err
			}
			ctx, out, errOut := cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()

			update, clear := progress(isQuiet(cfg))
			findings, errs := scanDisk(ctx, env, cfg, cfg.MinSize, update)
			clear()
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, e := range errs {
				_, _ = fmt.Fprintln(errOut, "warning:", e)
			}

			items := engine.BuildPlan(findings, engine.PlanOptions{Categories: cats, OlderThan: age, Now: time.Now()})
			if len(items) == 0 {
				_, _ = fmt.Fprintln(out, "Nothing to clean.")
				return &exitError{code: ExitNothing}
			}
			cleaner := newCleaner(env)
			cleaner.Guard = cfg.Guard(cleaner.Guard) // exclusions hold even if a finding slipped through
			opts := engine.ExecOptions{Permanent: permanent, DryRun: dryRun}

			// Dry runs and --yes use the default selection, the same list a real --yes run acts on.
			if dryRun || yes {
				sel := engine.Selected(items)
				if skipped := len(items) - len(sel); skipped > 0 && !isQuiet(cfg) {
					_, _ = fmt.Fprintf(errOut, "%d items not selected (review or careful risk, or their tool is running). Run without --yes to choose them.\n", skipped)
				}
				if len(sel) == 0 {
					_, _ = fmt.Fprintln(out, "Nothing selected by default.")
					return &exitError{code: ExitNothing}
				}
			} else {
				if !isInteractive() {
					return errors.New("clean needs a terminal to pick items; use --yes to apply the safe defaults")
				}
				var confirmed bool
				if items, confirmed, err = runChecklist(items, permanent); err != nil {
					return err
				}
				if !confirmed {
					_, _ = fmt.Fprintln(errOut, "Cancelled.")
					return &exitError{code: ExitCancelled}
				}
			}

			results := cleaner.Execute(ctx, items, opts)
			if dryRun {
				return ui.RenderPlan(out, results, ColorEnabled())
			}
			if err := ui.RenderResults(out, results, ColorEnabled()); err != nil {
				return err
			}
			for _, r := range results {
				if r.Outcome == engine.Failed {
					return &exitError{code: ExitError}
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be cleaned without changing anything")
	c.Flags().BoolVar(&yes, "yes", false, "skip the prompt and clean the safe defaults")
	c.Flags().BoolVar(&permanent, "permanent", false, "delete for good instead of using the Trash")
	c.Flags().StringSliceVar(&categories, "category", nil, "only list model, cache, tmp or logs findings")
	c.Flags().StringVar(&olderThan, "older-than", "", "only list findings last used longer ago than this, e.g. 30d")
	return c
}
