package detectors

import (
	"context"
	"path/filepath"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

// PkgCaches finds npm, pnpm, yarn, pip, uv and Go build caches.
// Paths checked against the spec table; verify on a real machine per tool version.
type PkgCaches struct{}

func (PkgCaches) Name() string { return "pkg_caches" }

func (PkgCaches) Scan(ctx context.Context, env engine.Env) ([]engine.Finding, error) {
	cache := paths.CacheDir(env)
	npm := paths.NpmCache(ctx, env)

	var pnpm, yarn, pip, uv string
	switch env.OS {
	case "windows":
		pnpm = filepath.Join(cache, "pnpm", "store")
		yarn = filepath.Join(cache, "Yarn", "Cache")
		pip = filepath.Join(cache, "pip", "Cache")
		uv = filepath.Join(cache, "uv", "cache")
	case "darwin":
		pnpm = filepath.Join(env.Home, "Library", "pnpm", "store")
		yarn = filepath.Join(cache, "Yarn")
		pip = filepath.Join(cache, "pip")
		uv = filepath.Join(env.Home, ".cache", "uv") // uv uses ~/.cache even on macOS
	default:
		data := orEnv(env, "XDG_DATA_HOME", filepath.Join(env.Home, ".local", "share"))
		pnpm = filepath.Join(data, "pnpm", "store")
		yarn = filepath.Join(cache, "yarn")
		pip = filepath.Join(cache, "pip")
		uv = filepath.Join(cache, "uv")
	}
	if v := env.Getenv("PIP_CACHE_DIR"); v != "" {
		pip = v
	}
	if v := env.Getenv("UV_CACHE_DIR"); v != "" {
		uv = v
	}

	c, s := engine.CategoryCache, engine.RiskSafe
	return measureTargets(ctx, "pkg_caches", []target{
		{"npm", "npm cache", filepath.Join(npm, "_cacache"), c, s},
		{"npm", "npx cache", filepath.Join(npm, "_npx"), c, s},
		{"pnpm", "pnpm store", pnpm, c, s},
		{"yarn", "Yarn cache", yarn, c, s},
		{"pip", "pip cache", pip, c, s},
		{"uv", "uv cache", uv, c, s},
		{"Go", "Go build cache", paths.GoCache(ctx, env), c, s},
	})
}

func orEnv(env engine.Env, key, def string) string {
	if v := env.Getenv(key); v != "" {
		return v
	}
	return def
}
