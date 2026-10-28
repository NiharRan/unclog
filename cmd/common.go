package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/NiharRan/unclog/internal/config"
	"github.com/NiharRan/unclog/internal/detectors"
	"github.com/NiharRan/unclog/internal/engine"
	"github.com/spf13/cobra"
)

// loadConfig reads the config file and prints its warnings (unknown keys) to stderr.
func loadConfig(cmd *cobra.Command, env engine.Env) (config.Config, error) {
	cfg, err := config.Load(env)
	for _, w := range cfg.Warnings {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}
	return cfg, err
}

// scanDisk runs every detector plus the configured custom paths and drops anything on the
// safety denylist or in the config exclusions. Findings smaller than min are hidden.
func scanDisk(ctx context.Context, env engine.Env, cfg config.Config, min engine.Size, onDone func(done, total int, name string)) ([]engine.Finding, []error) {
	ds := append([]engine.Detector(nil), detectorList()...)
	if len(cfg.CustomPaths) > 0 {
		ds = append(ds, detectors.Custom{Paths: cfg.CustomPaths})
	}
	allowed := keepAllowed(env)
	fs, errs := engine.ScanProgress(ctx, env, ds, func(f engine.Finding) bool { return cfg.Keep(f) && allowed(f) }, onDone)
	return engine.FilterMinSize(fs, min), errs
}

func newTracker(cfg config.Config) *engine.CPUTracker {
	return engine.NewCPUTracker(time.Duration(cfg.OrphanIdleMinutes) * time.Minute)
}

func isQuiet(cfg config.Config) bool { return quiet || cfg.Quiet }
