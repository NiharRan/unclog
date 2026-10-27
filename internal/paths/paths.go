// Package paths resolves OS-specific locations, applying env overrides before defaults.
// It switches on Env.OS rather than build tags so every OS layout is testable on one machine.
package paths

import (
	"context"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
)

const (
	windows = "windows"
	darwin  = "darwin"
)

// CacheDir is the per-user cache root.
func CacheDir(env engine.Env) string {
	switch env.OS {
	case windows:
		return orDefault(env.Getenv("LOCALAPPDATA"), filepath.Join(env.Home, "AppData", "Local"))
	case darwin:
		return filepath.Join(env.Home, "Library", "Caches")
	}
	return orDefault(env.Getenv("XDG_CACHE_HOME"), filepath.Join(env.Home, ".cache"))
}

// ConfigDir is the per-user config root (unclog's own config and history live under it).
func ConfigDir(env engine.Env) string {
	if env.OS == windows {
		return orDefault(env.Getenv("APPDATA"), filepath.Join(env.Home, "AppData", "Roaming"))
	}
	return orDefault(env.Getenv("XDG_CONFIG_HOME"), filepath.Join(env.Home, ".config"))
}

// AppSupportDir is where desktop apps (Cursor, VS Code, Windsurf) keep their data.
func AppSupportDir(env engine.Env) string {
	if env.OS == darwin {
		return filepath.Join(env.Home, "Library", "Application Support")
	}
	return ConfigDir(env)
}

// TempDirs lists temp roots to scan, in priority order, without duplicates.
func TempDirs(env engine.Env) []string {
	var cands []string
	switch env.OS {
	case windows:
		cands = []string{env.Getenv("TEMP"), env.Getenv("TMP")}
	case darwin:
		cands = []string{env.Getenv("TMPDIR"), "/tmp"}
	default:
		cands = []string{env.Getenv("TMPDIR"), "/tmp", "/var/tmp"}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		if c = filepath.Clean(c); c != "." && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// HFHome is the Hugging Face home; models live in its "hub" subfolder.
func HFHome(env engine.Env) string {
	if v := env.Getenv("HF_HOME"); v != "" {
		return v
	}
	if v := env.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "huggingface")
	}
	return filepath.Join(env.Home, ".cache", "huggingface")
}

// OllamaModels is the Ollama models dir.
func OllamaModels(env engine.Env) string {
	return orDefault(env.Getenv("OLLAMA_MODELS"), filepath.Join(env.Home, ".ollama", "models"))
}

// GoCache is the Go build cache: GOCACHE, then `go env GOCACHE`, then the OS default.
func GoCache(ctx context.Context, env engine.Env) string {
	if v := env.Getenv("GOCACHE"); v != "" {
		return v
	}
	if v := runPath(ctx, env, "go", "env", "GOCACHE"); v != "" {
		return v
	}
	return filepath.Join(CacheDir(env), "go-build")
}

// NpmCache is the npm cache: npm_config_cache, then `npm config get cache`, then the OS default.
func NpmCache(ctx context.Context, env engine.Env) string {
	if v := env.Getenv("npm_config_cache"); v != "" {
		return v
	}
	if v := runPath(ctx, env, "npm", "config", "get", "cache"); v != "" {
		return v
	}
	if env.OS == windows {
		return filepath.Join(CacheDir(env), "npm-cache")
	}
	return filepath.Join(env.Home, ".npm")
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// runPath returns a command's output only if it succeeded and is an absolute path.
func runPath(ctx context.Context, env engine.Env, name string, args ...string) string {
	if env.Run == nil {
		return ""
	}
	out, err := env.Run(ctx, name, args...)
	if err != nil || !filepath.IsAbs(out) {
		return ""
	}
	return out
}
