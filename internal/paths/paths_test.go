package paths

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
)

func env(goos string, vars map[string]string) engine.Env {
	return engine.Env{
		Home:   filepath.FromSlash("/home/u"),
		OS:     goos,
		Getenv: func(k string) string { return vars[k] },
	}
}

func eq(t *testing.T, name, got, want string) {
	t.Helper()
	// Compare in slash form: env overrides come back verbatim, defaults use the host separator.
	if filepath.ToSlash(got) != filepath.ToSlash(want) {
		t.Errorf("%s = %q; want %q", name, got, want)
	}
}

func TestCacheDir(t *testing.T) {
	eq(t, "darwin", CacheDir(env("darwin", nil)), "/home/u/Library/Caches")
	eq(t, "linux default", CacheDir(env("linux", nil)), "/home/u/.cache")
	eq(t, "linux xdg", CacheDir(env("linux", map[string]string{"XDG_CACHE_HOME": "/x"})), "/x")
	eq(t, "windows default", CacheDir(env("windows", nil)), "/home/u/AppData/Local")
	eq(t, "windows env", CacheDir(env("windows", map[string]string{"LOCALAPPDATA": "/L"})), "/L")
}

func TestConfigAndAppSupport(t *testing.T) {
	eq(t, "config linux", ConfigDir(env("linux", nil)), "/home/u/.config")
	eq(t, "config xdg", ConfigDir(env("linux", map[string]string{"XDG_CONFIG_HOME": "/c"})), "/c")
	eq(t, "config windows", ConfigDir(env("windows", map[string]string{"APPDATA": "/A"})), "/A")
	eq(t, "appsupport darwin", AppSupportDir(env("darwin", nil)), "/home/u/Library/Application Support")
	eq(t, "appsupport linux", AppSupportDir(env("linux", nil)), "/home/u/.config")
}

func TestTempDirs(t *testing.T) {
	got := TempDirs(env("linux", map[string]string{"TMPDIR": "/tmp"}))
	if len(got) != 2 || got[0] != filepath.FromSlash("/tmp") || got[1] != filepath.FromSlash("/var/tmp") {
		t.Errorf("linux temp dirs = %v", got)
	}
	if got := TempDirs(env("windows", nil)); len(got) != 0 {
		t.Errorf("windows with no TEMP = %v; want none", got)
	}
}

func TestHFHome(t *testing.T) {
	eq(t, "default", HFHome(env("linux", nil)), "/home/u/.cache/huggingface")
	eq(t, "xdg", HFHome(env("linux", map[string]string{"XDG_CACHE_HOME": "/x"})), "/x/huggingface")
	eq(t, "HF_HOME wins", HFHome(env("linux", map[string]string{"HF_HOME": "/hf", "XDG_CACHE_HOME": "/x"})), "/hf")
}

func TestOllamaModels(t *testing.T) {
	eq(t, "default", OllamaModels(env("linux", nil)), "/home/u/.ollama/models")
	eq(t, "override", OllamaModels(env("linux", map[string]string{"OLLAMA_MODELS": "/m"})), "/m")
}

func TestGoCache(t *testing.T) {
	ctx := context.Background()
	eq(t, "env wins", GoCache(ctx, env("linux", map[string]string{"GOCACHE": "/g"})), "/g")

	e := env("linux", nil)
	fromGo := filepath.Join(t.TempDir(), "from", "go") // must be absolute on every OS, drive letter included
	e.Run = func(context.Context, string, ...string) (string, error) { return fromGo, nil }
	eq(t, "command", GoCache(ctx, e), fromGo)

	e.Run = func(context.Context, string, ...string) (string, error) { return "", errors.New("no go") }
	eq(t, "fallback", GoCache(ctx, e), "/home/u/.cache/go-build")

	e.Run = func(context.Context, string, ...string) (string, error) { return "relative/junk", nil }
	eq(t, "non-absolute output ignored", GoCache(ctx, e), "/home/u/.cache/go-build")
}

func TestNpmCache(t *testing.T) {
	ctx := context.Background()
	eq(t, "env wins", NpmCache(ctx, env("linux", map[string]string{"npm_config_cache": "/n"})), "/n")
	eq(t, "unix default", NpmCache(ctx, env("linux", nil)), "/home/u/.npm")
	eq(t, "windows default", NpmCache(ctx, env("windows", nil)), "/home/u/AppData/Local/npm-cache")

	e := env("linux", nil)
	fromNpm := filepath.Join(t.TempDir(), "from", "npm")
	e.Run = func(context.Context, string, ...string) (string, error) { return fromNpm, nil }
	eq(t, "command", NpmCache(ctx, e), fromNpm)
}
