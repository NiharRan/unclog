package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
	"github.com/spf13/cobra"
)

var watchInterval = 2 * time.Second

func newPSCmd() *cobra.Command {
	var asJSON, watch, orphansOnly bool
	c := &cobra.Command{
		Use:   "ps",
		Short: "Live table of AI-related processes with an orphan flag",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON && watch {
				return errors.New("--json cannot be combined with --watch")
			}
			env, err := newEnv()
			if err != nil {
				return err
			}
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			tr := engine.NewCPUTracker(engine.DefaultIdleWindow)

			frame := func() (int, error) {
				procs, mem, err := procScan(ctx, env, tr)
				if err != nil {
					return 0, err
				}
				if orphansOnly {
					procs = onlyOrphans(procs)
				}
				now := time.Now()
				if asJSON {
					return len(procs), ui.WritePSJSON(out, procs, mem, now)
				}
				return len(procs), ui.RenderPS(out, procs, mem, ui.TableOptions{Color: ColorEnabled(), Now: now})
			}

			if !watch {
				n, err := frame()
				if err != nil {
					return err
				}
				if n == 0 {
					return &exitError{code: ExitNothing}
				}
				return nil
			}
			return watchLoop(cmd, out, frame)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON only")
	c.Flags().BoolVar(&watch, "watch", false, "refresh until interrupted")
	c.Flags().BoolVar(&orphansOnly, "orphans", false, "show only orphaned processes")
	return c
}

func onlyOrphans(procs []engine.Proc) []engine.Proc {
	var out []engine.Proc
	for _, p := range procs {
		if p.Orphan {
			out = append(out, p)
		}
	}
	return out
}

// watchLoop redraws one frame per interval until the context is cancelled (Ctrl-C).
// On a terminal each frame replaces the last; otherwise frames are separated by a blank line.
func watchLoop(cmd *cobra.Command, out io.Writer, frame func() (int, error)) error {
	ctx := cmd.Context()
	tty := isTerminal(os.Stdout)
	tick := time.NewTicker(watchInterval)
	defer tick.Stop()
	for {
		if tty {
			_, _ = fmt.Fprint(out, "\033[H\033[J")
		}
		if _, err := frame(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !tty {
			_, _ = fmt.Fprintln(out)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
