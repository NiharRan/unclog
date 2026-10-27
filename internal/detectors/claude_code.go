package detectors

import (
	"context"
	"os"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
)

// ClaudeCode reports session logs, shell snapshots and todos under ~/.claude
// (or CLAUDE_CONFIG_DIR). Checked against Claude Code on macOS; todos is absent on newer versions.
// Config and auth files (settings.json, ~/.claude.json) and per-project memory are never reported.
type ClaudeCode struct{}

func (ClaudeCode) Name() string { return "claude_code" }

func (ClaudeCode) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	root := orEnv(env, "CLAUDE_CONFIG_DIR", filepath.Join(env.Home, ".claude"))
	logs, review := engine.CategoryLogs, engine.RiskReview

	ts := []target{
		{"Claude Code", "shell snapshots", filepath.Join(root, "shell-snapshots"), logs, review},
		{"Claude Code", "todos", filepath.Join(root, "todos"), logs, review},
	}
	ts = append(ts, claudeProjects(filepath.Join(root, "projects"))...)

	fs, err := measureTargets(ctx, "claude_code", ts)
	markRunning(fs, env, "claude")
	return fs, err
}

// claudeProjects returns one target per project dir, so the user can pick. A project that
// holds a memory dir is split into its other entries, so cleaning can never remove memory.
func claudeProjects(dir string) []target {
	projects, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var ts []target
	logs, review := engine.CategoryLogs, engine.RiskReview
	for _, p := range projects {
		if !p.IsDir() { // false for symlinks
			continue
		}
		pdir := filepath.Join(dir, p.Name())
		label := "projects/" + p.Name()
		if _, err := os.Lstat(filepath.Join(pdir, "memory")); err != nil {
			ts = append(ts, target{"Claude Code", label, pdir, logs, review})
			continue
		}
		children, _ := os.ReadDir(pdir)
		for _, c := range children {
			if c.Name() != "memory" && c.Type()&os.ModeSymlink == 0 {
				ts = append(ts, target{"Claude Code", label + "/" + c.Name(), filepath.Join(pdir, c.Name()), logs, review})
			}
		}
	}
	return ts
}

// markRunning flags findings whose tool has a live process.
func markRunning(fs []engine.Finding, env engine.Env, names ...string) {
	if env.ProcessNames == nil || !safety.NameMatches(env.ProcessNames(), names...) {
		return
	}
	for i := range fs {
		fs[i].ToolRunning = true
	}
}
