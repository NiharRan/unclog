package detectors

import (
	"context"
	"os"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
)

// LMStudio reports one finding per model under ~/.lmstudio/models/<publisher>/<model>.
// Unverified: LM Studio is not installed on the dev machine; the layout is from its docs.
type LMStudio struct{}

func (LMStudio) Name() string { return "lmstudio" }

func (LMStudio) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	root := filepath.Join(env.Home, ".lmstudio", "models")
	publishers, err := os.ReadDir(root)
	if err != nil {
		return nil, nil
	}
	var ts []target
	for _, p := range publishers {
		if !p.IsDir() { // false for symlinks
			continue
		}
		models, _ := os.ReadDir(filepath.Join(root, p.Name()))
		for _, m := range models {
			if m.IsDir() {
				ts = append(ts, target{"LM Studio", p.Name() + "/" + m.Name(), filepath.Join(root, p.Name(), m.Name()),
					engine.CategoryModel, engine.RiskCareful})
			}
		}
	}
	fs, err := measureTargets(ctx, "lmstudio", ts)
	markRunning(fs, env, "LM Studio", "lm-studio")
	return fs, err
}
