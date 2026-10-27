package detectors

import (
	"context"

	"github.com/NiharRan/unclog/internal/engine"
)

// Custom reports user-configured directories (config custom_paths). Everything is risk
// review: unclog knows nothing about what they hold. The safety denylist still applies.
type Custom struct{ Paths []string }

func (Custom) Name() string { return "custom" }

func (c Custom) Scan(ctx context.Context, _ engine.Env) ([]engine.Finding, error) {
	ts := make([]target, len(c.Paths))
	for i, p := range c.Paths {
		ts[i] = target{"Custom paths", p, p, engine.CategoryCache, engine.RiskReview}
	}
	return measureTargets(ctx, "custom", ts)
}
