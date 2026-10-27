package cmd

import (
	"context"
	"time"

	"github.com/NiharRan/unclog/internal/detectors"
	"github.com/NiharRan/unclog/internal/engine"
)

// procScan snapshots AI processes and system memory; overridable in tests.
var procScan = func(ctx context.Context, env engine.Env, tr *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
	now := time.Now()
	all, err := engine.Snapshot(ctx, detectors.Interesting)
	if err != nil {
		return nil, engine.MemInfo{}, err
	}
	tr.Observe(all, now)
	return engine.Analyze(all, detectors.MatchAIProcess, tr, now), engine.ReadMem(ctx, env), nil
}
