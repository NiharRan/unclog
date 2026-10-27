package cmd

import (
	"context"
	"fmt"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
	"github.com/spf13/cobra"
)

// readDisk reports free space for the volume holding path; overridable in tests.
var readDisk = engine.ReadDisk

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Top 5 problems with a one-line fix for each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			cfg, err := loadConfig(cmd, env)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var warnings []string
			warn := func(e error) {
				warnings = append(warnings, e.Error())
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning:", e)
			}

			clear := progress(asJSON || isQuiet(cfg))
			findings, errs := scanDisk(ctx, env, cfg, cfg.MinSize)
			for _, e := range errs {
				warn(e)
			}
			procs, mem, perr := procScan(ctx, env, newTracker(cfg))
			if perr != nil {
				warn(fmt.Errorf("processes: %w", perr))
			}
			dsk := readDisk(context.Background(), env.Home)
			clear()
			if err := ctx.Err(); err != nil {
				return err
			}

			problems := engine.Diagnose(findings, procs, mem, dsk)
			out := cmd.OutOrStdout()
			if asJSON {
				err = ui.WriteDoctorJSON(out, problems, mem, dsk, warnings)
			} else {
				err = ui.RenderDoctor(out, problems, ColorEnabled())
			}
			if err != nil {
				return err
			}
			if len(problems) == 0 {
				return &exitError{code: ExitNothing}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON only")
	return c
}
