package detectors

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

const tmpMinAge = 24 * time.Hour

// Tmp finds top-level entries in the temp dirs that are owned by the current user and
// untouched for 24h. The open-file check happens at clean time (safety.Validate).
type Tmp struct{}

func (Tmp) Name() string { return "tmp" }

func (Tmp) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	var ts []target
	for _, dir := range paths.TempDirs(env) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fi, err := e.Info() // lstat: symlinks stay symlinks
			if err != nil || (!fi.IsDir() && !fi.Mode().IsRegular()) || !ownedByCurrentUser(fi) {
				continue
			}
			// A recent top-level mtime proves recent activity; deeper changes are caught below.
			if time.Since(fi.ModTime()) < tmpMinAge {
				continue
			}
			ts = append(ts, target{"Temp files", e.Name(), filepath.Join(dir, e.Name()), engine.CategoryTmp, engine.RiskSafe})
		}
	}
	fs, err := measureTargets(ctx, "tmp", ts)
	var old []engine.Finding
	for _, f := range fs {
		if time.Since(f.LastUsed) >= tmpMinAge {
			old = append(old, f)
		}
	}
	return old, err
}
