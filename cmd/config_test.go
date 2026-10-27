package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/config"
	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
)

// useConfigHome points the CLI at a temp home and config dir and returns the config file path.
func useConfigHome(t *testing.T, vars map[string]string) (home, file string) {
	t.Helper()
	home, cfgDir := t.TempDir(), t.TempDir()
	env := engine.Env{Home: home, OS: "linux", Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return cfgDir
		}
		return vars[k]
	}}
	old := newEnv
	newEnv = func() (engine.Env, error) { return env, nil }
	t.Cleanup(func() { newEnv = old })
	return home, config.Path(env)
}

func writeCfg(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPathInitAndShow(t *testing.T) {
	_, file := useConfigHome(t, nil)
	if out := run(t, "config", "path"); strings.TrimSpace(out) != file {
		t.Errorf("path = %q; want %q", out, file)
	}
	if out := run(t, "config"); !strings.Contains(out, "not found, using defaults") || !strings.Contains(out, "orphan_idle_minutes: 10") {
		t.Errorf("show before init:\n%s", out)
	}
	if out := run(t, "config", "init"); !strings.Contains(out, "Wrote "+file) {
		t.Errorf("init:\n%s", out)
	}
	if b, _ := os.ReadFile(file); string(b) != config.DefaultYAML {
		t.Error("init did not write the template")
	}
	writeCfg(t, file, "quiet: true\n")
	if _, err := runErr("config", "init"); exitCode(err) != ExitError {
		t.Error("init must refuse to overwrite")
	}
	if b, _ := os.ReadFile(file); string(b) != "quiet: true\n" {
		t.Error("init overwrote an existing config")
	}
	if out := run(t, "config"); !strings.Contains(out, "loaded") || !strings.Contains(out, "quiet: true") {
		t.Errorf("show after edit:\n%s", out)
	}
}

func TestConfigWorksEvenWhenConfigIsInvalid(t *testing.T) {
	_, file := useConfigHome(t, nil)
	writeCfg(t, file, "min_size: lots\n")
	if out := run(t, "config", "path"); strings.TrimSpace(out) != file {
		t.Error("`config path` must work with a broken config")
	}
	if _, err := runErr("config", "init"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("init err = %v", err)
	}
	if _, err := runErr("config"); err == nil || !strings.Contains(err.Error(), "min_size") {
		t.Errorf("show should report the problem: %v", err)
	}
}

func TestConfigEdit(t *testing.T) {
	_, file := useConfigHome(t, map[string]string{"EDITOR": "myeditor --wait"})
	var gotEditor, gotFile string
	old := runEditor
	runEditor = func(editor, f string) error { gotEditor, gotFile = editor, f; return nil }
	t.Cleanup(func() { runEditor = old })

	if _, err := runErr("config", "edit"); err != nil {
		t.Fatal(err)
	}
	if gotEditor != "myeditor --wait" || gotFile != file {
		t.Errorf("editor=%q file=%q", gotEditor, gotFile)
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("edit should create the default file when it is missing")
	}

	// a mistake made in the editor is reported right away
	runEditor = func(_, f string) error { return os.WriteFile(f, []byte("orphan_idle_minutes: 0\n"), 0o644) }
	if _, err := runErr("config", "edit"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v", err)
	}
}

func TestConfigEditWithoutEditor(t *testing.T) {
	_, file := useConfigHome(t, nil)
	_, err := runErr("config", "edit")
	if exitCode(err) != ExitError || !strings.Contains(err.Error(), "$EDITOR") || !strings.Contains(err.Error(), file) {
		t.Errorf("err = %v", err)
	}
	if _, serr := os.Stat(file); serr == nil {
		t.Error("no file should be created when there is nothing to open it with")
	}

	useConfigHome(t, map[string]string{"VISUAL": "vis"})
	old := runEditor
	var got string
	runEditor = func(editor, _ string) error { got = editor; return nil }
	t.Cleanup(func() { runEditor = old })
	if _, err := runErr("config", "edit"); err != nil || got != "vis" {
		t.Errorf("VISUAL: err=%v editor=%q", err, got)
	}
}

func TestInvalidConfigStopsEveryCommand(t *testing.T) {
	_, file := useConfigHome(t, nil)
	useDetectorsKeepEnv(t)
	writeCfg(t, file, "orphan_idle_minutes: ten\n")
	for _, args := range [][]string{{"scan"}, {"ps"}, {"clean", "--yes"}, {"kill", "--orphans"}, {"doctor"}} {
		if _, err := runErr(args...); exitCode(err) != ExitError || err == nil || !strings.Contains(err.Error(), "orphan_idle_minutes") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestUnknownKeyWarnsButWorks(t *testing.T) {
	_, file := useConfigHome(t, nil)
	useDetectorsKeepEnv(t, fake{name: "a", fs: []engine.Finding{finding("/x/.cache/pip", 5<<20)}})
	writeCfg(t, file, "colour: red\n")
	out, err := runErr("scan")
	if err != nil || !strings.Contains(out, `warning: unknown key "colour"`) {
		t.Errorf("err=%v out=%s", err, out)
	}
}

// useDetectorsKeepEnv installs fake detectors and processes but keeps the env set by useConfigHome.
func useDetectorsKeepEnv(t *testing.T, ds ...engine.Detector) {
	t.Helper()
	env, _ := newEnv()
	useDetectors(t, ds...)
	newEnv = func() (engine.Env, error) { return env, nil }
}

func TestExclusionsHideFromScanAndProtectClean(t *testing.T) {
	home, file := useConfigHome(t, nil)
	kept := filepath.Join(home, ".cache", "uv")
	excluded := filepath.Join(home, ".cache", "pip")
	for _, p := range []string{kept, excluded} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0o644)
	}
	fs := []engine.Finding{
		{Tool: "uv", Label: "uv", Path: kept, Size: 1 << 20, Category: engine.CategoryCache, Risk: engine.RiskSafe},
		{Tool: "pip", Label: "pip", Path: excluded, Size: 1 << 20, Category: engine.CategoryCache, Risk: engine.RiskSafe},
	}
	useDetectorsKeepEnv(t, fake{name: "a", fs: fs})
	writeCfg(t, file, "exclude:\n  - ~/.cache/pip\n")

	out, err := runErr("scan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc ui.ScanJSON
	_ = json.Unmarshal([]byte(out), &doc)
	if len(doc.Findings) != 1 || doc.Findings[0].Path != kept {
		t.Errorf("scan findings = %+v", doc.Findings)
	}

	// Even a finding that bypasses the scan filter is refused by the cleaner's guard.
	env, _ := newEnv()
	cfg, _ := config.Load(env)
	cl := newCleaner(env)
	cl.Guard = cfg.Guard(cl.Guard)
	res := cl.Execute(t.Context(), []engine.Item{{Finding: fs[1], Selected: true}}, engine.ExecOptions{Permanent: true})
	if res[0].Outcome != engine.Refused || !strings.Contains(res[0].Err.Error(), "excluded by config") {
		t.Errorf("result = %+v", res[0])
	}
	if _, err := os.Stat(excluded); err != nil {
		t.Fatal("excluded path was removed")
	}
}

func TestCleanNeverTouchesExcluded(t *testing.T) {
	ce := useClean(t)
	keep := ce.add(t, "keepme", engine.CategoryCache, engine.RiskSafe)
	gone := ce.add(t, "goner", engine.CategoryCache, engine.RiskSafe)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{keep, gone}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return filepath.Join(ce.home, "cfg")
		}
		return ""
	}}
	newEnv = func() (engine.Env, error) { return env, nil }
	writeCfg(t, config.Path(env), "exclude: [\"keepme\"]\n")

	if _, err := runErr("clean", "--yes"); err != nil {
		t.Fatal(err)
	}
	if !exists(keep.Path) || exists(gone.Path) {
		t.Errorf("excluded kept=%v other removed=%v", exists(keep.Path), !exists(gone.Path))
	}
}

func TestCustomPathsAndMinSizeFromConfig(t *testing.T) {
	home, file := useConfigHome(t, nil)
	custom := filepath.Join(home, "my-tool-cache")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(custom, "f"), make([]byte, 4096), 0o644)
	useDetectorsKeepEnv(t, fake{name: "a", fs: []engine.Finding{finding(filepath.Join(home, ".cache", "tiny"), 10)}})

	writeCfg(t, file, "custom_paths: [\"~/my-tool-cache\", \""+filepath.Join(home, "Documents")+"\"]\n")
	out, _ := runErr("scan", "--json")
	var doc ui.ScanJSON
	_ = json.Unmarshal([]byte(out), &doc)
	var paths []string
	for _, f := range doc.Findings {
		paths = append(paths, f.Path)
	}
	if len(doc.Findings) != 2 || !strings.Contains(strings.Join(paths, " "), "my-tool-cache") {
		t.Errorf("findings = %v", paths)
	}
	for _, f := range doc.Findings {
		if strings.HasSuffix(f.Path, "Documents") {
			t.Error("the denylist must still block a custom path inside Documents")
		}
		if f.Detector == "custom" && f.Risk != "review" {
			t.Errorf("custom path risk = %s", f.Risk)
		}
	}

	writeCfg(t, file, "min_size: 1KB\ncustom_paths: [\"~/my-tool-cache\"]\n")
	out, _ = runErr("scan", "--json")
	doc = ui.ScanJSON{}
	_ = json.Unmarshal([]byte(out), &doc)
	if len(doc.Findings) != 1 || doc.Findings[0].Detector != "custom" {
		t.Errorf("min_size from config: %+v", doc.Findings)
	}
	out, _ = runErr("scan", "--json", "--min-size", "1B") // the flag overrides the config
	doc = ui.ScanJSON{}
	_ = json.Unmarshal([]byte(out), &doc)
	if len(doc.Findings) != 2 {
		t.Errorf("--min-size should override config: %+v", doc.Findings)
	}
}

func TestQuietFromConfigHidesFooter(t *testing.T) {
	_, file := useConfigHome(t, nil)
	useDetectorsKeepEnv(t, fake{name: "a", fs: []engine.Finding{finding("/x/.cache/pip", 5<<20)}})
	if out, _ := runErr("scan"); !strings.Contains(out, "waitlist") {
		t.Fatal("footer expected by default")
	}
	writeCfg(t, file, "quiet: true\n")
	if out, _ := runErr("scan"); strings.Contains(out, "waitlist") {
		t.Errorf("quiet: true should hide the footer:\n%s", out)
	}
}

func TestOrphanIdleMinutesFromConfigReachesTracker(t *testing.T) {
	_, file := useConfigHome(t, nil)
	useDetectorsKeepEnv(t)
	var window int64
	procScanOld := procScan
	procScan = func(_ context.Context, _ engine.Env, tr *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
		window = int64(tr.Window.Minutes())
		return nil, engine.MemInfo{}, nil
	}
	t.Cleanup(func() { procScan = procScanOld })

	writeCfg(t, file, "orphan_idle_minutes: 45\n")
	_, _ = runErr("ps")
	if window != 45 {
		t.Errorf("tracker window = %d minutes; want 45", window)
	}
}
