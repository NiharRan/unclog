package detectors

import (
	"context"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

// Editor reports caches and logs of a VS Code-family editor under the app-support dir.
// Never reports User/settings.json, keybindings, snippets, globalStorage or extensions.
// Verified on macOS with Cursor, VS Code and Windsurf: workspaceStorage lives under User/, not at the top level.
type Editor struct {
	name    string   // detector name
	tool    string   // shown to the user
	dir     string   // folder under the app-support dir
	process []string // process names for the running check
}

func NewVSCode() Editor   { return Editor{"vscode", "VS Code", "Code", []string{"code"}} }
func NewWindsurf() Editor { return Editor{"windsurf", "Windsurf", "Windsurf", []string{"windsurf"}} }

func (e Editor) Name() string { return e.name }

func (e Editor) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	base := filepath.Join(paths.AppSupportDir(env), e.dir)
	c, logs, safe, review := engine.CategoryCache, engine.CategoryLogs, engine.RiskSafe, engine.RiskReview
	t := func(label, rel string, cat engine.Category, risk engine.Risk) target {
		return target{e.tool, label, filepath.Join(base, filepath.FromSlash(rel)), cat, risk}
	}
	fs, err := measureTargets(ctx, e.name, []target{
		t("cache", "Cache", c, safe),
		t("cached data", "CachedData", c, safe),
		t("code cache", "Code Cache", c, safe),
		t("GPU cache", "GPUCache", c, safe),
		t("extension downloads", "CachedExtensionVSIXs", c, safe),
		t("logs", "logs", logs, safe),
		t("workspace storage", "User/workspaceStorage", c, review),
		t("local file history", "User/History", logs, review),
	})
	markRunning(fs, env, e.process...)
	return fs, err
}
