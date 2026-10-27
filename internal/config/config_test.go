package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
)

func testEnv(t *testing.T, goos string) engine.Env {
	t.Helper()
	cfgDir := t.TempDir()
	vars := map[string]string{"XDG_CONFIG_HOME": cfgDir, "APPDATA": cfgDir}
	return engine.Env{Home: "/home/u", OS: goos, Getenv: func(k string) string { return vars[k] }}
}

func write(t *testing.T, env engine.Env, content string) {
	t.Helper()
	file := Path(env)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingAndEmptyFileGiveDefaults(t *testing.T) {
	env := testEnv(t, "linux")
	cfg, err := Load(env)
	if err != nil || cfg.Exists || cfg.OrphanIdleMinutes != 10 || cfg.MinSize != 0 || len(cfg.Exclude) != 0 {
		t.Fatalf("missing file: %+v, %v", cfg, err)
	}
	for _, content := range []string{"", "\n", "# only a comment\n"} {
		write(t, env, content)
		cfg, err := Load(env)
		if err != nil || !cfg.Exists || cfg.OrphanIdleMinutes != 10 {
			t.Errorf("content %q: %+v, %v", content, cfg, err)
		}
	}
}

func TestLoadAllKeys(t *testing.T) {
	env := testEnv(t, "linux")
	write(t, env, `
exclude:
  - ~/.cache/pip
  - "*.log"
custom_paths: ["~/.cache/my-tool", "/opt/data"]
min_size: 100MB
orphan_idle_minutes: 30
quiet: true
update_check: true
`)
	cfg, err := Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Exclude, []string{"/home/u/.cache/pip", "*.log"}) ||
		!slices.Equal(cfg.CustomPaths, []string{"/home/u/.cache/my-tool", "/opt/data"}) ||
		cfg.MinSize != 100<<20 || cfg.OrphanIdleMinutes != 30 || !cfg.Quiet || !cfg.UpdateCheck {
		t.Errorf("cfg = %+v", cfg)
	}
	write(t, env, "min_size: 2048\n")
	if cfg, _ := Load(env); cfg.MinSize != 2048 {
		t.Errorf("numeric min_size = %d", cfg.MinSize)
	}
}

func TestInvalidValuesGiveClearErrors(t *testing.T) {
	for content, want := range map[string]string{
		"min_size: lots\n":                "min_size",
		"min_size: -5\n":                  "min_size",
		"orphan_idle_minutes: 0\n":        "orphan_idle_minutes",
		"orphan_idle_minutes: ten\n":      "orphan_idle_minutes",
		"orphan_idle_minutes: 99999\n":    "orphan_idle_minutes",
		"quiet: maybe\n":                  "quiet",
		"update_check: 1\n":               "update_check",
		"exclude: ~/.cache\n":             "exclude",
		"exclude: [1, 2]\n":               "exclude",
		"custom_paths: [relative/dir]\n":  "custom_paths",
		"custom_paths: {a: b}\n":          "custom_paths",
		"exclude: [\"\"]\n":               "exclude",
		"this: is: not: yaml\n  - oops\n": "config.yaml",
	} {
		env := testEnv(t, "linux")
		write(t, env, content)
		_, err := Load(env)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "config.yaml") {
			t.Errorf("%q: err = %v; want it to mention %q and the file", content, err, want)
		}
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	env := testEnv(t, "linux")
	write(t, env, "quiet: true\nmin_siz: 1GB\ncolour: red\n")
	cfg, err := Load(env)
	if err != nil || !cfg.Quiet {
		t.Fatalf("%+v, %v", cfg, err)
	}
	if len(cfg.Warnings) != 2 || !strings.Contains(cfg.Warnings[0], "colour") || !strings.Contains(cfg.Warnings[1], "min_siz") {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
}

func TestDefaultYAMLIsValid(t *testing.T) {
	env := testEnv(t, "linux")
	write(t, env, DefaultYAML)
	cfg, err := Load(env)
	if err != nil || len(cfg.Warnings) != 0 || cfg.OrphanIdleMinutes != 10 {
		t.Fatalf("default template does not load cleanly: %+v, %v", cfg, err)
	}
	for _, k := range Keys() {
		if !strings.Contains(DefaultYAML, k+":") {
			t.Errorf("template does not document %s", k)
		}
	}
}

func TestWriteRefusesToOverwrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := Write(file); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != DefaultYAML {
		t.Error("file does not hold the default template")
	}
	if err := os.WriteFile(file, []byte("quiet: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(file); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(file); string(b) != "quiet: true\n" {
		t.Error("existing config was overwritten")
	}
}

func TestPathHonoursXDGAndAppData(t *testing.T) {
	env := engine.Env{Home: "/home/u", OS: "linux", Getenv: func(k string) string { return map[string]string{"XDG_CONFIG_HOME": "/x"}[k] }}
	if got, want := Path(env), filepath.FromSlash("/x/unclog/config.yaml"); got != want {
		t.Errorf("path = %q; want %q", got, want)
	}
	env.OS, env.Getenv = "windows", func(k string) string { return map[string]string{"APPDATA": "/A"}[k] }
	if got, want := Path(env), filepath.FromSlash("/A/unclog/config.yaml"); got != want {
		t.Errorf("windows path = %q; want %q", got, want)
	}
}

func cfgWith(goos string, exclude ...string) Config {
	c := Default()
	c.home, c.Exclude = "/home/u", exclude
	c.fold, c.win = goos == "darwin" || goos == "windows", goos == "windows"
	return c
}

func TestExcluded(t *testing.T) {
	c := cfgWith("linux", "/home/u/.cache/pip", "/home/u/work/*/node_modules", "*.log", "secret")
	for p, want := range map[string]bool{
		"/home/u/.cache/pip":                true,  // the path itself
		"/home/u/.cache/pip/http/a":         true,  // inside it
		"/home/u/.cache":                    true,  // removing it would remove the excluded dir
		"/home/u/.cache/uv":                 false, // sibling
		"/home/u/.cache/pipx":               false, // prefix of the name only
		"/home/u/work/app/node_modules":     true,
		"/home/u/work/app/node_modules/x/y": true,
		"/home/u/work/app/src":              false,
		"/home/u/work":                      false, // a glob never excludes a parent
		"/var/tmp/build.log":                true,  // bare pattern matches any depth
		"/var/tmp/build.logs":               false,
		"/home/u/a/secret/b":                true,
		"/home/u/secretary":                 false,
	} {
		if got := c.Excluded(p); got != want {
			t.Errorf("Excluded(%q) = %v; want %v", p, got, want)
		}
	}
	if (Config{}).Excluded("/anything") {
		t.Error("no exclusions must exclude nothing")
	}
}

func TestExcludedCaseAndSeparators(t *testing.T) {
	mac := cfgWith("darwin", "/Users/u/Library/Caches/Foo")
	if !mac.Excluded("/users/U/library/caches/foo/bar") {
		t.Error("macOS matching is case-insensitive")
	}
	if cfgWith("linux", "/home/u/Cache").Excluded("/home/u/cache") {
		t.Error("Linux matching is case-sensitive")
	}
	win := cfgWith("windows", `C:\Users\u\AppData\Local\Foo`)
	if !win.Excluded(`c:\users\u\appdata\local\foo\cache`) {
		t.Error("Windows matching ignores case and slash direction")
	}
}

func TestKeepAndGuard(t *testing.T) {
	c := cfgWith("linux", "/home/u/.cache/pip")
	if c.Keep(engine.Finding{Path: "/home/u/.cache/pip"}) || !c.Keep(engine.Finding{Path: "/home/u/.cache/uv"}) {
		t.Error("Keep misjudged")
	}
	if !c.Keep(engine.Finding{CleanVia: []string{"docker"}}) {
		t.Error("findings without a path are not subject to path exclusions")
	}

	g := c.Guard(passGuard{})
	err := g.Validate(engine.Finding{Path: "/home/u/.cache/pip/x"}, "/home/u/.cache/pip")
	if !errors.Is(err, safety.ErrRefused) || !strings.Contains(err.Error(), "excluded by config") {
		t.Errorf("err = %v", err)
	}
	if err := g.Validate(engine.Finding{Path: "/home/u/.cache/uv"}, "/home/u/.cache"); err != nil {
		t.Errorf("non-excluded path refused: %v", err)
	}
}

type passGuard struct{}

func (passGuard) Validate(engine.Finding, string) error { return nil }
