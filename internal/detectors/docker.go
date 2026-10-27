package detectors

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
)

// Docker reports reclaimable images, containers and build cache from `docker system df`.
// Removal always goes through the docker CLI (CleanVia); raw docker files are never touched.
// Volumes are skipped on purpose: pruning them destroys data.
type Docker struct{}

func (Docker) Name() string { return "docker" }

var dockerPrune = map[string]struct {
	label string
	via   []string
}{
	"Images":      {"unused images", []string{"docker", "image", "prune", "-a", "-f"}},
	"Containers":  {"stopped containers", []string{"docker", "container", "prune", "-f"}},
	"Build Cache": {"build cache", []string{"docker", "builder", "prune", "-a", "-f"}},
}

func (Docker) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	out, err := env.Run(ctx, "docker", "system", "df", "--format", "{{json .}}")
	if err != nil { // not installed or daemon down: nothing to report
		return nil, nil
	}
	var fs []engine.Finding
	for _, line := range strings.Split(out, "\n") {
		var row struct{ Type, Reclaimable string }
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		p, ok := dockerPrune[row.Type]
		if !ok {
			continue
		}
		word, _, _ := strings.Cut(strings.TrimSpace(row.Reclaimable), " ") // "1.2GB (83%)"
		size, err := engine.ParseSize(word)
		if err != nil || size == 0 {
			continue
		}
		fs = append(fs, engine.Finding{
			Tool: "Docker", Label: p.label, Size: size, Category: engine.CategoryCache,
			Risk: engine.RiskReview, Detector: "docker", CleanVia: p.via,
		})
	}
	return fs, nil
}
