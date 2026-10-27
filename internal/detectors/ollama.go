package detectors

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

// Ollama reports one finding per model from `ollama list`, removed via `ollama rm`.
// Without the CLI it reports the models dir as a single finding.
type Ollama struct{}

func (Ollama) Name() string { return "ollama" }

var columnGap = regexp.MustCompile(`\s{2,}`)

func (Ollama) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	dirs := []string{paths.OllamaModels(env)}
	if env.OS == "linux" && env.Getenv("OLLAMA_MODELS") == "" {
		dirs = append(dirs, "/usr/share/ollama/.ollama/models")
	}
	var dir string
	for _, d := range dirs {
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() {
			dir = d
			break
		}
	}
	if dir == "" {
		return nil, nil
	}

	if out, err := env.Run(ctx, "ollama", "list"); err == nil {
		if fs := parseOllamaList(out, dir); len(fs) > 0 {
			return fs, nil
		}
	}
	return measureTargets(ctx, "ollama", []target{
		{"Ollama", "models", dir, engine.CategoryModel, engine.RiskCareful},
	})
}

// parseOllamaList reads `ollama list` rows: NAME, ID, SIZE, MODIFIED.
func parseOllamaList(out, dir string) []engine.Finding {
	var fs []engine.Finding
	lines := strings.Split(out, "\n")
	for _, l := range lines[min(1, len(lines)):] { // skip header
		cols := columnGap.Split(strings.TrimSpace(l), -1)
		if len(cols) < 3 {
			continue
		}
		size, err := engine.ParseSize(cols[2])
		if err != nil {
			continue
		}
		fs = append(fs, engine.Finding{
			Tool: "Ollama", Label: cols[0], Path: dir, Size: size, LastUsed: manifestTime(dir, cols[0]),
			Category: engine.CategoryModel, Risk: engine.RiskCareful, Detector: "ollama",
			CleanVia: []string{"ollama", "rm", cols[0]},
		})
	}
	return fs
}

// manifestTime is the mtime of the model's manifest, or zero if not found.
func manifestTime(dir, name string) time.Time {
	model, tag, ok := strings.Cut(name, ":")
	if !ok {
		tag = "latest"
	}
	ns := "library"
	if i := strings.LastIndex(model, "/"); i >= 0 {
		ns, model = model[:i], model[i+1:]
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "manifests", "*", ns, model, tag))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil {
			return fi.ModTime()
		}
	}
	return time.Time{}
}
