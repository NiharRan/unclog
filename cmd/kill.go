package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NiharRan/unclog/internal/detectors"
	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
	"github.com/NiharRan/unclog/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Overridable in tests.
var (
	newKiller = func(env engine.Env) *engine.Killer {
		return engine.NewKiller(detectors.MatchAIProcess, safety.NewHistory(env))
	}
	killGrace = engine.DefaultGrace
	// sampleGap separates the two CPU samples taken before orphans are killed.
	sampleGap = 2 * time.Second
	// askYes asks a yes/no question on the terminal.
	askYes = func(cmd *cobra.Command, q string) bool {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), q+" [y/N] ")
		line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		return strings.EqualFold(strings.TrimSpace(line), "y")
	}
	pickProcs = func(items []engine.Item) ([]engine.Item, bool, error) {
		m := ui.NewChecklist(items, false, ColorEnabled())
		m.Title, m.Verb = "Select processes to stop", "Terminate"
		if _, err := tea.NewProgram(m).Run(); err != nil {
			return nil, false, err
		}
		return m.Items(), m.Confirmed, nil
	}
)

func newKillCmd() *cobra.Command {
	var orphans, dryRun, yes bool
	var pids []int
	c := &cobra.Command{
		Use:   "kill",
		Short: "Stop orphaned AI processes (SIGTERM, then SIGKILL after 5s)",
		Long: `Stops processes that unclog recognises as AI tooling. Never touches other users'
processes, PID < 100, this program or its parent shell, or anything no detector matches.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			tr := engine.NewCPUTracker(engine.DefaultIdleWindow)

			procs, _, err := procScan(ctx, env, tr)
			if err != nil {
				return err
			}
			if orphans || len(pids) == 0 {
				// A second sample shows what is busy right now; busy processes are not orphans.
				time.Sleep(sampleGap)
				if procs, _, err = procScan(ctx, env, tr); err != nil {
					return err
				}
			}

			var targets []engine.Proc
			switch {
			case len(pids) > 0 || orphans:
				targets = pickTargets(procs, orphans, pids)
			case yes:
				targets = pickTargets(procs, true, nil)
			default:
				if !isInteractive() {
					return errors.New("kill needs a terminal to pick processes; use --orphans or --pid, with --yes to skip the prompt")
				}
				var confirmed bool
				if targets, confirmed, err = chooseProcs(procs); err != nil {
					return err
				}
				if !confirmed {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return &exitError{code: ExitCancelled}
				}
				yes = true // the checklist already confirmed
			}
			if len(targets) == 0 {
				_, _ = fmt.Fprintln(out, "Nothing to stop.")
				return &exitError{code: ExitNothing}
			}

			if !dryRun && !yes {
				if !isInteractive() {
					return errors.New("refusing to stop processes without confirmation; pass --yes")
				}
				var rss uint64
				for _, t := range targets {
					rss += t.RSS
				}
				if !askYes(cmd, fmt.Sprintf("Stop %d processes using about %s of RAM?", len(targets), engine.Size(rss))) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return &exitError{code: ExitCancelled}
				}
			}

			results := newKiller(env).Terminate(ctx, targets, engine.KillOptions{DryRun: dryRun, Grace: killGrace})
			if err := ui.RenderKill(out, results, dryRun, ColorEnabled()); err != nil {
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
	c.Flags().BoolVar(&orphans, "orphans", false, "stop all orphaned AI processes")
	c.Flags().IntSliceVar(&pids, "pid", nil, "stop this AI process (repeatable)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be stopped without sending signals")
	c.Flags().BoolVar(&yes, "yes", false, "skip the confirmation")
	return c
}

// pickTargets selects orphans that are idle right now and/or the requested PIDs. A PID that is
// not among the matched processes becomes a bare stub that the killer then refuses.
func pickTargets(procs []engine.Proc, orphans bool, pids []int) []engine.Proc {
	var out []engine.Proc
	seen := map[int32]bool{}
	add := func(p engine.Proc) {
		if !seen[p.PID] {
			seen[p.PID] = true
			out = append(out, p)
		}
	}
	for _, pid := range pids {
		found := false
		for _, p := range procs {
			if int(p.PID) == pid {
				add(p)
				found = true
			}
		}
		if !found {
			add(engine.Proc{PID: int32(pid)})
		}
	}
	if orphans {
		for _, p := range procs {
			if p.Orphan && engine.IdleNow(p) {
				add(p)
			}
		}
	}
	return out
}

// chooseProcs lets the user pick from all matched processes; idle orphans start checked.
func chooseProcs(procs []engine.Proc) ([]engine.Proc, bool, error) {
	if len(procs) == 0 {
		return nil, true, nil
	}
	// Group by tool (largest first) so the checklist's tool headers read correctly.
	rank := map[string]int{}
	for i, g := range engine.GroupByTool(procs) {
		rank[g.Tool] = i
	}
	sorted := append([]engine.Proc(nil), procs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Tool != sorted[j].Tool {
			return rank[sorted[i].Tool] < rank[sorted[j].Tool]
		}
		return sorted[i].RSS > sorted[j].RSS
	})
	items := make([]engine.Item, len(sorted))
	for i, p := range sorted {
		risk := engine.RiskReview
		if p.Orphan && engine.IdleNow(p) {
			risk = engine.RiskSafe
		}
		items[i] = engine.Item{
			Finding:  engine.Finding{Tool: p.Tool, Label: engine.ProcLabel(p), Size: engine.Size(p.RSS), Risk: risk},
			Selected: risk == engine.RiskSafe,
		}
	}
	items, confirmed, err := pickProcs(items)
	if err != nil || !confirmed {
		return nil, confirmed, err
	}
	var picked []engine.Proc
	for i, it := range items {
		if it.Selected {
			picked = append(picked, sorted[i])
		}
	}
	return picked, true, nil
}
