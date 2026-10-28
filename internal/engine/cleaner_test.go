package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
)

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func f(tool, label string, size engine.Size, cat engine.Category, risk engine.Risk) engine.Finding {
	return engine.Finding{Tool: tool, Label: label, Path: "/h/" + label, Size: size, Category: cat, Risk: risk, LastUsed: now.Add(-48 * time.Hour)}
}

func TestBuildPlanDefaultSelection(t *testing.T) {
	running := f("Cursor", "cache", 5, engine.CategoryCache, engine.RiskSafe)
	running.ToolRunning = true
	items := engine.BuildPlan([]engine.Finding{
		f("pip", "pip cache", 10, engine.CategoryCache, engine.RiskSafe),
		f("HF", "model", 30, engine.CategoryModel, engine.RiskCareful),
		f("Claude", "logs", 20, engine.CategoryLogs, engine.RiskReview),
		running,
	}, engine.PlanOptions{Now: now})

	got := map[string]bool{}
	for _, it := range items {
		got[it.Label] = it.Selected
	}
	want := map[string]bool{"pip cache": true, "model": false, "logs": false, "cache": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s selected = %v; want %v", k, got[k], v)
		}
	}
	if items[0].Tool != "HF" { // largest group first
		t.Errorf("order = %v", items)
	}
}

func TestBuildPlanFilters(t *testing.T) {
	old := f("a", "old", 1, engine.CategoryCache, engine.RiskSafe)
	old.LastUsed = now.Add(-40 * 24 * time.Hour)
	young := f("a", "young", 1, engine.CategoryCache, engine.RiskSafe)
	unknown := f("a", "unknown", 1, engine.CategoryModel, engine.RiskSafe)
	unknown.LastUsed = time.Time{}
	all := []engine.Finding{old, young, unknown}

	labels := func(items []engine.Item) []string {
		var l []string
		for _, it := range items {
			l = append(l, it.Label)
		}
		slices.Sort(l)
		return l
	}
	if got := labels(engine.BuildPlan(all, engine.PlanOptions{Now: now, OlderThan: 30 * 24 * time.Hour})); !slices.Equal(got, []string{"old"}) {
		t.Errorf("older-than: %v (unknown ages must be dropped)", got)
	}
	if got := labels(engine.BuildPlan(all, engine.PlanOptions{Now: now, Categories: map[engine.Category]bool{engine.CategoryModel: true}})); !slices.Equal(got, []string{"unknown"}) {
		t.Errorf("category: %v", got)
	}
}

func TestParseAgeAndCategories(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "12h": 12 * time.Hour, "1.5d": 36 * time.Hour} {
		if got, err := engine.ParseAge(in); err != nil || got != want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "soon", "-3d", "d"} {
		if _, err := engine.ParseAge(in); err == nil {
			t.Errorf("ParseAge(%q) should fail", in)
		}
	}
	got, err := engine.ParseCategories([]string{"models,Cache", "logs"})
	if err != nil || !got[engine.CategoryModel] || !got[engine.CategoryCache] || !got[engine.CategoryLogs] || got[engine.CategoryTmp] {
		t.Errorf("categories = %v, %v", got, err)
	}
	if _, err := engine.ParseCategories([]string{"stuff"}); err == nil {
		t.Error("unknown category should fail")
	}
}

// fixture: a temp home with a cache root, a fake trash, and a recording log.
type fixture struct {
	home, cache, trashDir string
	logs                  []string
	logErr                error
	c                     *engine.Cleaner
	execs                 [][]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{home: t.TempDir(), trashDir: t.TempDir()}
	fx.cache = filepath.Join(fx.home, ".cache")
	env := engine.Env{Home: fx.home, OS: "linux", Getenv: func(string) string { return "" }}
	fx.c = &engine.Cleaner{
		Guard: &safety.Checker{Env: env, Open: func() []string { return nil }, Procs: func() []string { return nil }},
		Log: logFunc(func(action, target string, size engine.Size, outcome string) error {
			if fx.logErr != nil {
				return fx.logErr
			}
			fx.logs = append(fx.logs, action+"|"+target+"|"+outcome)
			return nil
		}),
		Trash: func(p string) error { return os.Rename(p, filepath.Join(fx.trashDir, filepath.Base(p))) },
		Exec:  func(_ context.Context, argv []string) error { fx.execs = append(fx.execs, argv); return nil },
	}
	return fx
}

type logFunc func(action, target string, size engine.Size, outcome string) error

func (l logFunc) Log(a, t string, s engine.Size, o string) error { return l(a, t, s, o) }

func (fx *fixture) item(t *testing.T, name string, selected bool) engine.Item {
	t.Helper()
	p := filepath.Join(fx.cache, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return engine.Item{Finding: engine.Finding{Tool: "T", Label: name, Path: p, Size: 1, Risk: engine.RiskSafe}, Selected: selected}
}

func TestExecuteTrashesOnlySelected(t *testing.T) {
	fx := newFixture(t)
	a, b := fx.item(t, "a", true), fx.item(t, "b", false)
	res := fx.c.Execute(context.Background(), []engine.Item{a, b}, engine.ExecOptions{})

	if len(res) != 1 || res[0].Outcome != engine.Trashed || res[0].Err != nil {
		t.Fatalf("results = %+v", res)
	}
	if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
		t.Error("selected item still in place")
	}
	if _, err := os.Stat(b.Path); err != nil {
		t.Error("unselected item was touched")
	}
	if _, err := os.Stat(filepath.Join(fx.trashDir, "a", "data")); err != nil {
		t.Error("item did not reach the trash")
	}
	if want := []string{"trash|" + a.Path + "|started", "trash|" + a.Path + "|trashed"}; !slices.Equal(fx.logs, want) {
		t.Errorf("log = %v; want %v", fx.logs, want)
	}
	if engine.Freed(res) != 1 {
		t.Errorf("freed = %v", engine.Freed(res))
	}
}

func TestExecutePermanentDeletes(t *testing.T) {
	fx := newFixture(t)
	a := fx.item(t, "a", true)
	res := fx.c.Execute(context.Background(), []engine.Item{a}, engine.ExecOptions{Permanent: true})
	if res[0].Outcome != engine.Deleted || res[0].Action != "delete" {
		t.Fatalf("result = %+v", res[0])
	}
	if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
		t.Error("item still exists")
	}
	if entries, _ := os.ReadDir(fx.trashDir); len(entries) != 0 {
		t.Error("permanent delete must bypass the trash")
	}
}

func TestExecuteRefusesDeniedAndLogsIt(t *testing.T) {
	fx := newFixture(t)
	docs := filepath.Join(fx.home, "Documents", "precious")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	it := engine.Item{Finding: engine.Finding{Tool: "T", Label: "docs", Path: docs, Risk: engine.RiskSafe}, Selected: true}
	res := fx.c.Execute(context.Background(), []engine.Item{it}, engine.ExecOptions{Permanent: true})

	if res[0].Outcome != engine.Refused || !errors.Is(res[0].Err, safety.ErrRefused) {
		t.Fatalf("result = %+v", res[0])
	}
	if _, err := os.Stat(docs); err != nil {
		t.Fatal("denied path was deleted")
	}
	if len(fx.logs) != 1 || !strings.Contains(fx.logs[0], "refused") {
		t.Errorf("log = %v", fx.logs)
	}
}

func TestExecuteRefusesSymlinkSwappedAfterScan(t *testing.T) {
	fx := newFixture(t)
	a := fx.item(t, "a", true)
	victim := filepath.Join(fx.home, "Documents")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	// Between scan and clean the cache dir is replaced by a symlink to ~/Documents.
	if err := os.RemoveAll(a.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, a.Path); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	res := fx.c.Execute(context.Background(), []engine.Item{a}, engine.ExecOptions{Permanent: true})
	if res[0].Outcome != engine.Refused {
		t.Fatalf("result = %+v", res[0])
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("symlink target was deleted")
	}
}

func TestExecuteMissingPath(t *testing.T) {
	fx := newFixture(t)
	a := fx.item(t, "a", true)
	_ = os.RemoveAll(a.Path)
	if res := fx.c.Execute(context.Background(), []engine.Item{a}, engine.ExecOptions{}); res[0].Outcome != engine.Refused {
		t.Errorf("result = %+v", res[0])
	}
}

func TestDryRunListsSameItemsAndChangesNothing(t *testing.T) {
	fx := newFixture(t)
	items := []engine.Item{fx.item(t, "a", true), fx.item(t, "b", true), fx.item(t, "c", false)}

	dry := fx.c.Execute(context.Background(), items, engine.ExecOptions{DryRun: true})
	if len(fx.logs) != 0 {
		t.Errorf("dry run wrote to the log: %v", fx.logs)
	}
	for _, it := range items {
		if _, err := os.Stat(it.Path); err != nil {
			t.Fatalf("dry run changed %s", it.Path)
		}
	}
	real := fx.c.Execute(context.Background(), items, engine.ExecOptions{})

	if len(dry) != len(real) {
		t.Fatalf("dry %d items, real %d", len(dry), len(real))
	}
	for i := range dry {
		if dry[i].Finding.Path != real[i].Finding.Path || dry[i].Outcome != engine.DryRun || dry[i].Action != real[i].Action {
			t.Errorf("item %d differs: dry=%+v real=%+v", i, dry[i], real[i])
		}
	}
}

func TestExecuteStopsWhenLogUnwritable(t *testing.T) {
	fx := newFixture(t)
	a := fx.item(t, "a", true)
	fx.logErr = errors.New("disk full")
	res := fx.c.Execute(context.Background(), []engine.Item{a}, engine.ExecOptions{Permanent: true})
	if res[0].Outcome != engine.Failed {
		t.Fatalf("result = %+v", res[0])
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatal("item was removed although it could not be logged")
	}
}

func TestExecuteContinuesAfterFailure(t *testing.T) {
	fx := newFixture(t)
	a, b := fx.item(t, "a", true), fx.item(t, "b", true)
	n := 0
	fx.c.Trash = func(p string) error {
		if n++; n == 1 {
			return errors.New("trash unavailable")
		}
		return os.Rename(p, filepath.Join(fx.trashDir, filepath.Base(p)))
	}
	res := fx.c.Execute(context.Background(), []engine.Item{a, b}, engine.ExecOptions{})
	if res[0].Outcome != engine.Failed || res[1].Outcome != engine.Trashed {
		t.Fatalf("results = %+v", res)
	}
	if engine.Freed(res) != 1 {
		t.Errorf("freed counts failures: %v", engine.Freed(res))
	}
}

func TestExecuteCleanVia(t *testing.T) {
	fx := newFixture(t)
	ok := engine.Item{Finding: engine.Finding{Tool: "Docker", Label: "images", Size: 7, CleanVia: []string{"docker", "image", "prune", "-f"}}, Selected: true}
	bad := engine.Item{Finding: engine.Finding{Tool: "X", Label: "evil", Size: 7, CleanVia: []string{"rm", "-rf", "/"}}, Selected: true}
	res := fx.c.Execute(context.Background(), []engine.Item{ok, bad}, engine.ExecOptions{})

	if res[0].Outcome != engine.Ran || res[1].Outcome != engine.Refused {
		t.Fatalf("results = %+v", res)
	}
	if len(fx.execs) != 1 || fx.execs[0][0] != "docker" {
		t.Errorf("execs = %v", fx.execs)
	}
}

func TestExecuteStopsOnCancelledContext(t *testing.T) {
	fx := newFixture(t)
	a := fx.item(t, "a", true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := fx.c.Execute(ctx, []engine.Item{a}, engine.ExecOptions{}); len(res) != 0 {
		t.Errorf("results = %+v", res)
	}
}

func TestExecuteReportsProgress(t *testing.T) {
	fx := newFixture(t)
	a, b := fx.item(t, "a", true), fx.item(t, "b", true)
	var started []string
	var dones []int
	o := engine.ExecOptions{
		OnStart:  func(done, total int, f engine.Finding) { started = append(started, f.Label); _ = total },
		OnResult: func(done, total int, r engine.Result) { dones = append(dones, done); _ = total },
	}
	fx.c.Execute(context.Background(), []engine.Item{a, b}, o)
	if len(started) != 2 || len(dones) != 2 || dones[1] != 2 {
		t.Errorf("started=%v dones=%v", started, dones)
	}
}
