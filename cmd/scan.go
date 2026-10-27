package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/NiharRan/unclog/internal/detectors"
	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
	"github.com/NiharRan/unclog/internal/ui"
	"github.com/spf13/cobra"
)

// Overridable in tests.
var (
	newEnv       = engine.NewEnv
	detectorList = detectors.All
)

func newScanCmd() *cobra.Command {
	var asJSON bool
	var only, minSize string
	c := &cobra.Command{
		Use:   "scan",
		Short: "Show disk and RAM used by AI tooling, grouped by tool",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if only != "" && only != "disk" && only != "ram" {
				return fmt.Errorf("--only must be disk or ram, got %q", only)
			}
			var min engine.Size
			if minSize != "" {
				var err error
				if min, err = engine.ParseSize(minSize); err != nil {
					return fmt.Errorf("--min-size: %w", err)
				}
			}
			env, err := newEnv()
			if err != nil {
				return err
			}

			doDisk, doRAM := only != "ram", only != "disk"
			ctx := cmd.Context()
			var findings []engine.Finding
			var warnings []string
			warn := func(e error) {
				warnings = append(warnings, e.Error())
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning:", e)
			}

			clear := progress(asJSON)
			if doDisk {
				fs, errs := engine.Scan(ctx, env, detectorList(), keepAllowed(env))
				findings = engine.FilterMinSize(fs, min)
				for _, e := range errs {
					warn(e)
				}
			}
			var ram *ui.RAMReport
			if doRAM {
				procs, mem, err := procScan(ctx, env, engine.NewCPUTracker(engine.DefaultIdleWindow))
				if err != nil {
					warn(fmt.Errorf("processes: %w", err))
				} else {
					ram = &ui.RAMReport{Procs: procs, Mem: mem}
				}
			}
			clear()
			if err := ctx.Err(); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				err = ui.WriteScanJSON(out, findings, ram, warnings)
			} else {
				err = ui.RenderScan(out, findings, ram, ui.TableOptions{Color: ColorEnabled(), Quiet: quiet, Now: time.Now()})
			}
			if err != nil {
				return err
			}
			if len(findings) == 0 && (ram == nil || len(ram.Procs) == 0) {
				return &exitError{code: ExitNothing}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON only")
	c.Flags().StringVar(&only, "only", "", "limit to `disk` or `ram`")
	c.Flags().StringVar(&minSize, "min-size", "", "hide findings smaller than this, e.g. 100MB")
	return c
}

// progress shows a transient "Scanning…" on stderr, only when stderr is a terminal.
func progress(skip bool) (clear func()) {
	if skip || quiet || !isTerminal(os.Stderr) {
		return func() {}
	}
	_, _ = fmt.Fprint(os.Stderr, "Scanning…")
	return func() { _, _ = fmt.Fprint(os.Stderr, "\r\033[K") }
}

// keepAllowed drops findings whose path is on the safety denylist. Items with no path
// (Docker) are removed through a CLI and have nothing to check.
func keepAllowed(env engine.Env) func(engine.Finding) bool {
	return func(f engine.Finding) bool {
		if f.Path == "" && len(f.CleanVia) > 0 {
			return true
		}
		denied, _ := safety.IsDenied(f.Path, env)
		return !denied
	}
}
