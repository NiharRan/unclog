package detectors

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/testutil"
)

func testEnv(home, goos string, vars map[string]string, run func(name string, args ...string) (string, error)) engine.Env {
	env := engine.Env{Home: home, OS: goos, Getenv: func(k string) string { return vars[k] }}
	env.Run = func(_ context.Context, name string, args ...string) (string, error) {
		if run == nil {
			return "", errors.New("command not found")
		}
		return run(name, args...)
	}
	return env
}

func scan(t *testing.T, d engine.Detector, env engine.Env) map[string]engine.Finding {
	t.Helper()
	fs, err := d.Scan(context.Background(), env)
	if err != nil {
		t.Fatalf("%s: %v", d.Name(), err)
	}
	got := map[string]engine.Finding{}
	for _, f := range fs {
		if f.Detector != d.Name() || f.Tool == "" || f.Category == "" || f.Risk == "" || f.Size <= 0 {
			t.Errorf("incomplete finding %+v", f)
		}
		got[f.Label] = f
	}
	return got
}

func put(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantSizes(t *testing.T, got map[string]engine.Finding, want map[string]engine.Size) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %d findings %v; want %d %v", len(got), keys(got), len(want), want)
	}
	for label, size := range want {
		if f, ok := got[label]; !ok || f.Size != size {
			t.Errorf("%q = %+v; want size %d", label, f, size)
		}
	}
}

func keys(m map[string]engine.Finding) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func TestPkgCachesFakeHome(t *testing.T) {
	env := testEnv(testutil.FakeHome(t), "linux", nil, nil)
	got := scan(t, PkgCaches{}, env)
	wantSizes(t, got, map[string]engine.Size{
		"npm cache": 100, "npx cache": 50, "pip cache": 200, "uv cache": 300, "Yarn cache": 10, "Go build cache": 400,
	})
	for _, f := range got {
		if f.Risk != engine.RiskSafe || f.Category != engine.CategoryCache {
			t.Errorf("%s: want safe cache, got %s %s", f.Label, f.Risk, f.Category)
		}
	}
}

func TestPkgCachesOSLayouts(t *testing.T) {
	cases := []struct {
		goos string
		rel  map[string]string // path under home -> label
	}{
		{"darwin", map[string]string{
			"Library/Caches/pip": "pip cache", "Library/Caches/Yarn": "Yarn cache", ".cache/uv": "uv cache",
			"Library/pnpm/store": "pnpm store", ".npm/_cacache": "npm cache", "Library/Caches/go-build": "Go build cache",
		}},
		{"windows", map[string]string{
			"AppData/Local/pip/Cache": "pip cache", "AppData/Local/Yarn/Cache": "Yarn cache", "AppData/Local/uv/cache": "uv cache",
			"AppData/Local/pnpm/store": "pnpm store", "AppData/Local/npm-cache/_cacache": "npm cache", "AppData/Local/go-build": "Go build cache",
		}},
		{"linux", map[string]string{".local/share/pnpm/store": "pnpm store"}},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			home := t.TempDir()
			want := map[string]engine.Size{}
			for rel, label := range c.rel {
				put(t, filepath.Join(home, filepath.FromSlash(rel), "f"), 7)
				want[label] = 7
			}
			wantSizes(t, scan(t, PkgCaches{}, testEnv(home, c.goos, nil, nil)), want)
		})
	}
}

func TestPkgCachesEnvOverrides(t *testing.T) {
	home, custom := t.TempDir(), t.TempDir()
	for name, n := range map[string]int{"pip": 1, "go": 2, "npm/_cacache": 3, "uv": 4} {
		put(t, filepath.Join(custom, name, "f"), n)
	}
	// defaults exist too and must be ignored
	put(t, filepath.Join(home, ".cache", "pip", "f"), 99)
	env := testEnv(home, "linux", map[string]string{
		"PIP_CACHE_DIR": filepath.Join(custom, "pip"), "GOCACHE": filepath.Join(custom, "go"),
		"npm_config_cache": filepath.Join(custom, "npm"), "UV_CACHE_DIR": filepath.Join(custom, "uv"),
	}, nil)
	wantSizes(t, scan(t, PkgCaches{}, env), map[string]engine.Size{
		"pip cache": 1, "Go build cache": 2, "npm cache": 3, "uv cache": 4,
	})
}

func TestPkgCachesViaCommands(t *testing.T) {
	home, npm := t.TempDir(), t.TempDir()
	put(t, filepath.Join(npm, "_cacache", "f"), 5)
	env := testEnv(home, "linux", nil, func(name string, args ...string) (string, error) {
		if name == "npm" {
			return npm, nil
		}
		return "", errors.New("no")
	})
	wantSizes(t, scan(t, PkgCaches{}, env), map[string]engine.Size{"npm cache": 5})
}

func TestPkgCachesSymlinkedRootSkipped(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	put(t, filepath.Join(outside, "f"), 50)
	if err := os.MkdirAll(filepath.Join(home, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".cache", "pip")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	wantSizes(t, scan(t, PkgCaches{}, testEnv(home, "linux", nil, nil)), nil)
}

func TestEmptyHomeNoFindings(t *testing.T) {
	env := testEnv(t.TempDir(), "linux", nil, nil)
	for _, d := range []engine.Detector{PkgCaches{}, HuggingFace{}, Ollama{}} {
		wantSizes(t, scan(t, d, env), nil)
	}
}

func TestHuggingFace(t *testing.T) {
	home := testutil.FakeHome(t)
	got := scan(t, HuggingFace{}, testEnv(home, "linux", nil, nil))
	wantSizes(t, got, map[string]engine.Size{"org/alpha": 1020, "dataset org/data": 500})
	if f := got["org/alpha"]; f.Risk != engine.RiskCareful || f.Category != engine.CategoryModel || f.LastUsed.IsZero() {
		t.Errorf("org/alpha = %+v", f)
	}
}

func TestHuggingFaceEnvOverrides(t *testing.T) {
	home, hf, hub := testutil.FakeHome(t), t.TempDir(), t.TempDir()
	put(t, filepath.Join(hf, "hub", "models--a--b", "f"), 11)
	put(t, filepath.Join(hub, "models--c--d", "f"), 22)

	wantSizes(t, scan(t, HuggingFace{}, testEnv(home, "linux", map[string]string{"HF_HOME": hf}, nil)),
		map[string]engine.Size{"a/b": 11})
	wantSizes(t, scan(t, HuggingFace{}, testEnv(home, "linux", map[string]string{"HF_HOME": hf, "HF_HUB_CACHE": hub}, nil)),
		map[string]engine.Size{"c/d": 22})
}

func TestHuggingFaceSymlinkEscape(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	put(t, filepath.Join(outside, "f"), 50)
	hub := filepath.Join(home, ".cache", "huggingface", "hub")
	if err := os.MkdirAll(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(hub, "models--evil--repo")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	wantSizes(t, scan(t, HuggingFace{}, testEnv(home, "linux", nil, nil)), nil)
}

const ollamaList = `NAME             ID              SIZE      MODIFIED
llama3:latest    abc123def456    4.7 GB    2 weeks ago
user/foo:7b      def456abc123    1.0 GB    3 days ago
`

func TestOllamaWithCLI(t *testing.T) {
	home := testutil.FakeHome(t)
	env := testEnv(home, "linux", nil, func(name string, args ...string) (string, error) {
		if name == "ollama" && slices.Equal(args, []string{"list"}) {
			return ollamaList, nil
		}
		return "", errors.New("no")
	})
	got := scan(t, Ollama{}, env)
	llama, _ := engine.ParseSize("4.7 GB")
	wantSizes(t, got, map[string]engine.Size{"llama3:latest": llama, "user/foo:7b": 1 << 30})
	f := got["llama3:latest"]
	if !slices.Equal(f.CleanVia, []string{"ollama", "rm", "llama3:latest"}) {
		t.Errorf("CleanVia = %v", f.CleanVia)
	}
	if f.LastUsed.IsZero() {
		t.Error("llama3 manifest exists in the fixture; LastUsed should be set")
	}
	if !got["user/foo:7b"].LastUsed.IsZero() {
		t.Error("no manifest for user/foo; LastUsed should be zero")
	}
}

func TestOllamaWithoutCLI(t *testing.T) {
	got := scan(t, Ollama{}, testEnv(testutil.FakeHome(t), "linux", nil, nil))
	wantSizes(t, got, map[string]engine.Size{"models": 2100})
	if got["models"].CleanVia != nil {
		t.Error("dir finding must not carry CleanVia")
	}
}

func TestOllamaModelsOverride(t *testing.T) {
	home, custom := testutil.FakeHome(t), t.TempDir()
	put(t, filepath.Join(custom, "blobs", "x"), 33)
	wantSizes(t, scan(t, Ollama{}, testEnv(home, "linux", map[string]string{"OLLAMA_MODELS": custom}, nil)),
		map[string]engine.Size{"models": 33})
}

func TestOllamaEmptyListFallsBackToDir(t *testing.T) {
	env := testEnv(testutil.FakeHome(t), "linux", nil, func(string, ...string) (string, error) {
		return "NAME  ID  SIZE  MODIFIED\n", nil
	})
	wantSizes(t, scan(t, Ollama{}, env), map[string]engine.Size{"models": 2100})
}

func TestTmp(t *testing.T) {
	tmp, outside := t.TempDir(), t.TempDir()
	old := time.Now().Add(-72 * time.Hour)
	recent := time.Now().Add(-2 * time.Hour)
	age := func(p string, ts time.Time) {
		t.Helper()
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	put(t, filepath.Join(tmp, "old-file"), 10)
	age(filepath.Join(tmp, "old-file"), old)
	put(t, filepath.Join(tmp, "recent-file"), 10)
	age(filepath.Join(tmp, "recent-file"), recent)
	put(t, filepath.Join(tmp, "old-dir", "f"), 20)
	age(filepath.Join(tmp, "old-dir", "f"), old)
	age(filepath.Join(tmp, "old-dir"), old)
	// old top-level mtime but a recent file inside: still in use
	put(t, filepath.Join(tmp, "busy-dir", "f"), 30)
	age(filepath.Join(tmp, "busy-dir"), old)
	// symlink to something big and old
	put(t, filepath.Join(outside, "big"), 999)
	if err := os.Symlink(outside, filepath.Join(tmp, "link")); err == nil {
		age(outside, old)
	}

	got := scan(t, Tmp{}, testEnv(t.TempDir(), "windows", map[string]string{"TEMP": tmp}, nil))
	wantSizes(t, got, map[string]engine.Size{"old-file": 10, "old-dir": 20})
	for _, f := range got {
		if f.Risk != engine.RiskSafe || f.Category != engine.CategoryTmp {
			t.Errorf("%s: want safe tmp, got %s %s", f.Label, f.Risk, f.Category)
		}
	}
}

func TestTmpMissingDir(t *testing.T) {
	env := testEnv(t.TempDir(), "windows", map[string]string{"TEMP": filepath.Join(t.TempDir(), "nope")}, nil)
	wantSizes(t, scan(t, Tmp{}, env), nil)
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := testEnv(testutil.FakeHome(t), "linux", nil, nil)
	if _, err := (PkgCaches{}).Scan(ctx, env); err == nil {
		t.Error("PkgCaches should report the cancelled context")
	}
}

func TestAllHaveUniqueNames(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All() {
		if seen[d.Name()] {
			t.Errorf("duplicate detector name %q", d.Name())
		}
		seen[d.Name()] = true
	}
}

func TestClaudeCode(t *testing.T) {
	home := testutil.FakeHome(t)
	got := scan(t, ClaudeCode{}, testEnv(home, "linux", nil, nil))
	wantSizes(t, got, map[string]engine.Size{
		"projects/-home-u-projA":          1024,
		"projects/-home-u-projB/s1.jsonl": 300,
		"projects/-home-u-projB/s2":       40,
		"shell snapshots":                 60,
	})
	for label, f := range got {
		if f.Risk != engine.RiskReview || f.Category != engine.CategoryLogs {
			t.Errorf("%s: want review logs, got %s %s", label, f.Risk, f.Category)
		}
		// config, auth and memory must never be inside or be a finding
		for _, bad := range []string{"settings.json", ".claude.json", "memory"} {
			if filepath.Base(f.Path) == bad {
				t.Errorf("%s: reported %s", label, f.Path)
			}
		}
	}
}

func TestClaudeCodeConfigDirOverride(t *testing.T) {
	custom := t.TempDir()
	put(t, filepath.Join(custom, "shell-snapshots", "s"), 9)
	env := testEnv(testutil.FakeHome(t), "linux", map[string]string{"CLAUDE_CONFIG_DIR": custom}, nil)
	wantSizes(t, scan(t, ClaudeCode{}, env), map[string]engine.Size{"shell snapshots": 9})
}

func TestClaudeCodeSymlinkedProjectSkipped(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	put(t, filepath.Join(outside, "f"), 50)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".claude", "projects", "evil")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	wantSizes(t, scan(t, ClaudeCode{}, testEnv(home, "linux", nil, nil)), nil)
}

func TestEditors(t *testing.T) {
	home := testutil.FakeHome(t)
	env := testEnv(home, "linux", nil, nil)

	got := scan(t, NewCursor(), env)
	wantSizes(t, got, map[string]engine.Size{"cache": 10, "cached data": 20, "logs": 30, "workspace storage": 40})
	if got["cache"].Risk != engine.RiskSafe || got["logs"].Risk != engine.RiskSafe || got["workspace storage"].Risk != engine.RiskReview {
		t.Errorf("risks wrong: %+v", got)
	}
	for _, f := range got {
		if f.Tool != "Cursor" {
			t.Errorf("tool = %q", f.Tool)
		}
		// settings and globalStorage live next to workspaceStorage and must not be reported
		for _, bad := range []string{"settings.json", "globalStorage"} {
			if filepath.Base(f.Path) == bad {
				t.Errorf("reported %s", f.Path)
			}
		}
	}

	wantSizes(t, scan(t, NewVSCode(), env), map[string]engine.Size{"cache": 11, "workspace storage": 41, "local file history": 8})
	wantSizes(t, scan(t, NewWindsurf(), env), map[string]engine.Size{"cached data": 22})
}

func TestEditorsOSLayouts(t *testing.T) {
	for goos, rel := range map[string]string{
		"darwin":  "Library/Application Support/Cursor/Cache",
		"windows": "AppData/Roaming/Cursor/Cache",
		"linux":   ".config/Cursor/Cache",
	} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			put(t, filepath.Join(home, filepath.FromSlash(rel), "f"), 7)
			wantSizes(t, scan(t, NewCursor(), testEnv(home, goos, nil, nil)), map[string]engine.Size{"cache": 7})
		})
	}
}

func TestEditorsXDGOverride(t *testing.T) {
	cfg := t.TempDir()
	put(t, filepath.Join(cfg, "Cursor", "Cache", "f"), 3)
	env := testEnv(testutil.FakeHome(t), "linux", map[string]string{"XDG_CONFIG_HOME": cfg}, nil)
	wantSizes(t, scan(t, NewCursor(), env), map[string]engine.Size{"cache": 3})
}

func TestToolRunningFlag(t *testing.T) {
	env := testEnv(testutil.FakeHome(t), "linux", nil, nil)
	env.ProcessNames = func() []string { return []string{"Cursor Helper (Renderer)", "bash"} }
	for _, f := range scan(t, NewCursor(), env) {
		if !f.ToolRunning {
			t.Errorf("Cursor finding %q not flagged while Cursor runs", f.Label)
		}
	}
	for _, f := range scan(t, NewVSCode(), env) {
		if f.ToolRunning {
			t.Errorf("VS Code finding %q flagged although only Cursor runs", f.Label)
		}
	}
	env.ProcessNames = func() []string { return []string{"claude"} }
	for _, f := range scan(t, ClaudeCode{}, env) {
		if !f.ToolRunning {
			t.Errorf("Claude finding %q not flagged", f.Label)
		}
	}
}

func TestLMStudio(t *testing.T) {
	got := scan(t, LMStudio{}, testEnv(testutil.FakeHome(t), "linux", nil, nil))
	wantSizes(t, got, map[string]engine.Size{"pub/model-a": 500, "pub/model-b": 600})
	for _, f := range got {
		if f.Risk != engine.RiskCareful || f.Category != engine.CategoryModel {
			t.Errorf("%s: want careful model, got %s %s", f.Label, f.Risk, f.Category)
		}
	}
}

const dockerDF = `{"Type":"Images","TotalCount":"5","Active":"2","Size":"2.1GB","Reclaimable":"1.5GB (71%)"}
{"Type":"Containers","TotalCount":"3","Active":"1","Size":"10MB","Reclaimable":"10MB (100%)"}
{"Type":"Local Volumes","TotalCount":"2","Active":"0","Size":"900MB","Reclaimable":"900MB (100%)"}
{"Type":"Build Cache","TotalCount":"9","Active":"0","Size":"300kB","Reclaimable":"300kB"}
not json at all
`

func TestDocker(t *testing.T) {
	var gotArgs []string
	env := testEnv(t.TempDir(), "linux", nil, func(name string, args ...string) (string, error) {
		gotArgs = append([]string{name}, args...)
		return dockerDF, nil
	})
	got := scan(t, Docker{}, env)
	wantSizes(t, got, map[string]engine.Size{"unused images": 3 << 29, "stopped containers": 10 << 20, "build cache": 300 << 10})
	if gotArgs[0] != "docker" {
		t.Errorf("ran %v", gotArgs)
	}
	for label, f := range got {
		if f.Path != "" || len(f.CleanVia) < 3 || f.CleanVia[0] != "docker" || f.Risk != engine.RiskReview {
			t.Errorf("%s: %+v", label, f)
		}
	}
	for _, f := range got {
		if f.Label == "local volumes" {
			t.Error("volumes must never be offered")
		}
	}
}

func TestDockerUnavailable(t *testing.T) {
	for name, run := range map[string]func(string, ...string) (string, error){
		"nil runner / not installed": nil,
		"daemon down":                func(string, ...string) (string, error) { return "", errors.New("Cannot connect to the Docker daemon") },
		"empty output":               func(string, ...string) (string, error) { return "", nil },
		"missing reclaimable field":  func(string, ...string) (string, error) { return `{"Type":"Images"}`, nil },
		"nothing reclaimable":        func(string, ...string) (string, error) { return `{"Type":"Images","Reclaimable":"0B (0%)"}`, nil },
	} {
		t.Run(name, func(t *testing.T) {
			wantSizes(t, scan(t, Docker{}, testEnv(t.TempDir(), "linux", nil, run)), nil)
		})
	}
}
