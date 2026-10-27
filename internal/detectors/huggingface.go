package detectors

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

// HuggingFace reports one finding per cached repo under the hub cache.
// shortcut: last used is the newest file mtime (download time), not access time; upgrade if atime proves reliable.
type HuggingFace struct{}

func (HuggingFace) Name() string { return "huggingface" }

var hfPrefixes = map[string]string{"models--": "", "datasets--": "dataset ", "spaces--": "space "}

func (HuggingFace) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	hub := filepath.Join(paths.HFHome(env), "hub")
	for _, k := range []string{"HF_HUB_CACHE", "HUGGINGFACE_HUB_CACHE"} {
		if v := env.Getenv(k); v != "" {
			hub = v
			break
		}
	}
	entries, err := os.ReadDir(hub)
	if err != nil {
		return nil, nil
	}
	var ts []target
	for _, e := range entries {
		if !e.IsDir() { // DirEntry.IsDir is false for symlinks
			continue
		}
		for prefix, kind := range hfPrefixes {
			if rest, ok := strings.CutPrefix(e.Name(), prefix); ok {
				label := kind + strings.ReplaceAll(rest, "--", "/")
				ts = append(ts, target{"Hugging Face", label, filepath.Join(hub, e.Name()), engine.CategoryModel, engine.RiskCareful})
			}
		}
	}
	return measureTargets(ctx, "huggingface", ts)
}
