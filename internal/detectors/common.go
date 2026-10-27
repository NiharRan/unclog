// Package detectors holds one detector per tool or cache type. Detectors are read-only.
package detectors

import (
	"context"
	"os"
	"sync"

	"github.com/NiharRan/unclog/internal/engine"
)

// All returns every detector shipped in this build.
func All() []engine.Detector {
	return []engine.Detector{
		PkgCaches{}, Tmp{}, HuggingFace{}, Ollama{},
		ClaudeCode{}, NewCursor(), NewVSCode(), NewWindsurf(), LMStudio{}, Docker{},
	}
}

type target struct {
	tool, label, path string
	category          engine.Category
	risk              engine.Risk
}

const measureWorkers = 4

// measureTargets sizes each target concurrently. Missing paths, symlinks and empty
// targets yield no finding, so a machine without the tool is not an error.
func measureTargets(ctx context.Context, detector string, ts []target) ([]engine.Finding, error) {
	out := make([]*engine.Finding, len(ts))
	sem := make(chan struct{}, measureWorkers)
	var wg sync.WaitGroup
	for i, t := range ts {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fi, err := os.Lstat(t.path)
			if err != nil || fi.Mode()&os.ModeSymlink != 0 {
				return
			}
			size, last, err := engine.Measure(ctx, t.path)
			if err != nil || size == 0 {
				return
			}
			out[i] = &engine.Finding{
				Tool: t.tool, Label: t.label, Path: t.path, Size: size, LastUsed: last,
				Category: t.category, Risk: t.risk, Detector: detector,
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var fs []engine.Finding
	for _, f := range out {
		if f != nil {
			fs = append(fs, *f)
		}
	}
	return fs, nil
}
