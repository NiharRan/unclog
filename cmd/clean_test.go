package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
)

type cleanEnv struct {
	home, trash string
	logs        []string
}

// useClean wires a temp home, fake trash and recording log into the clean command.
func useClean(t *testing.T) *cleanEnv {
	t.Helper()
	ce := &cleanEnv{home: t.TempDir(), trash: t.TempDir()}
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}

	oldEnv, oldC, oldI, oldR := newEnv, newCleaner, isInteractive, runChecklist
	newEnv = func() (engine.Env, error) { return env, nil }
	newCleaner = func(engine.Env) *engine.Cleaner {
		return &engine.Cleaner{
			Guard: &safety.Checker{Env: env, Open: func() []string { return nil }, Procs: func() []string { return nil }},
			Log: logRec(func(a, tg string, _ engine.Size, o string) error {
				ce.logs = append(ce.logs, a+"|"+filepath.Base(tg)+"|"+o)
				return nil
			}),
			Trash: func(p string) (string, error) { return "", os.Rename(p, filepath.Join(ce.trash, filepath.Base(p))) },
			Exec:  func(context.Context, []string) error { return nil },
		}
	}
	isInteractive = func() bool { return true }
	t.Cleanup(func() { newEnv, newCleaner, isInteractive, runChecklist = oldEnv, oldC, oldI, oldR })
	useProcs(t, nil)
	return ce
}

type logRec func(a, tg string, s engine.Size, o string) error

func (l logRec) Log(a, tg string, s engine.Size, o string) error { return l(a, tg, s, o) }

// add creates a real directory under home/.cache and returns a finding for it.
func (ce *cleanEnv) add(t *testing.T, name string, cat engine.Category, risk engine.Risk) engine.Finding {
	t.Helper()
	p := filepath.Join(ce.home, ".cache", name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return engine.Finding{Tool: "T", Label: name, Path: p, Size: 1 << 20, Category: cat, Risk: risk, LastUsed: time.Now().Add(-48 * time.Hour)}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestCleanYesTrashesOnlySafe(t *testing.T) {
	ce := useClean(t)
	safeF := ce.add(t, "pipcache", engine.CategoryCache, engine.RiskSafe)
	model := ce.add(t, "model", engine.CategoryModel, engine.RiskCareful)
	running := ce.add(t, "running", engine.CategoryCache, engine.RiskSafe)
	running.ToolRunning = true
	useDetectors(t, fake{name: "a", fs: []engine.Finding{safeF, model, running}})
	// useDetectors replaced newEnv; restore the temp-home env
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }

	out, err := runErr("clean", "--yes", "--no-color")
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if exists(safeF.Path) || !exists(model.Path) || !exists(running.Path) {
		t.Errorf("safe gone=%v model kept=%v running kept=%v", !exists(safeF.Path), exists(model.Path), exists(running.Path))
	}
	if !exists(filepath.Join(ce.trash, "pipcache", "f")) {
		t.Error("safe item did not reach the trash")
	}
	if !strings.Contains(out, "Moved 1 item (1.0 MB) to the Trash.") {
		t.Errorf("output:\n%s", out)
	}
	if len(ce.logs) != 2 {
		t.Errorf("log = %v", ce.logs)
	}
}

func TestCleanDryRunChangesNothingAndMatchesYes(t *testing.T) {
	ce := useClean(t)
	a := ce.add(t, "a", engine.CategoryCache, engine.RiskSafe)
	b := ce.add(t, "b", engine.CategoryCache, engine.RiskSafe)
	review := ce.add(t, "review", engine.CategoryLogs, engine.RiskReview)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{a, b, review}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }

	dry, err := runErr("clean", "--dry-run", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(a.Path) || !exists(b.Path) || len(ce.logs) != 0 {
		t.Fatal("dry run changed something")
	}
	real, _ := runErr("clean", "--yes", "--no-color")
	for _, name := range []string{"a", "b"} {
		if !strings.Contains(dry, "T: "+name) {
			t.Errorf("dry run is missing %s:\n%s", name, dry)
		}
		if !strings.Contains(real, "T: "+name) {
			t.Errorf("real run is missing %s:\n%s", name, real)
		}
	}
	if strings.Contains(dry, "T: review") || strings.Contains(real, "T: review") {
		t.Error("review item must not be in the default selection")
	}
	if !strings.Contains(dry, "Dry run: 2 items") {
		t.Errorf("dry output:\n%s", dry)
	}
}

func TestCleanNeedsTerminalWithoutYes(t *testing.T) {
	ce := useClean(t)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{ce.add(t, "a", engine.CategoryCache, engine.RiskSafe)}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }
	isInteractive = func() bool { return false }

	if _, err := runErr("clean"); exitCode(err) != ExitError {
		t.Errorf("code = %d; want %d", exitCode(err), ExitError)
	}
}

func TestCleanInteractiveSelectionAndCancel(t *testing.T) {
	ce := useClean(t)
	a := ce.add(t, "a", engine.CategoryCache, engine.RiskSafe)
	model := ce.add(t, "model", engine.CategoryModel, engine.RiskCareful)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{a, model}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }

	// the user also ticks the careful model, then confirms
	runChecklist = func(items []engine.Item, _ bool) ([]engine.Item, bool, error) {
		for i := range items {
			items[i].Selected = true
		}
		return items, true, nil
	}
	if _, err := runErr("clean", "--no-color"); err != nil {
		t.Fatal(err)
	}
	if exists(a.Path) || exists(model.Path) {
		t.Error("both ticked items should be trashed")
	}

	ce2 := useClean(t)
	b := ce2.add(t, "b", engine.CategoryCache, engine.RiskSafe)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{b}})
	env2 := engine.Env{Home: ce2.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env2, nil }
	runChecklist = func(items []engine.Item, _ bool) ([]engine.Item, bool, error) { return items, false, nil }
	_, err := runErr("clean")
	if exitCode(err) != ExitCancelled || !exists(b.Path) {
		t.Errorf("cancel: code=%d kept=%v", exitCode(err), exists(b.Path))
	}
}

func TestCleanNothingExitCodes(t *testing.T) {
	useClean(t)
	useDetectors(t)
	if _, err := runErr("clean", "--yes"); exitCode(err) != ExitNothing {
		t.Errorf("nothing found: code=%d", exitCode(err))
	}

	ce := useClean(t)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{ce.add(t, "model", engine.CategoryModel, engine.RiskCareful)}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }
	out, err := runErr("clean", "--yes", "--category", "models")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "Nothing selected by default.") {
		t.Errorf("careful-only with --yes: code=%d out=%q", exitCode(err), out)
	}
}

func TestCleanFilters(t *testing.T) {
	ce := useClean(t)
	cache := ce.add(t, "cache", engine.CategoryCache, engine.RiskSafe)
	logs := ce.add(t, "logs", engine.CategoryLogs, engine.RiskSafe)
	oldF := ce.add(t, "oldlogs", engine.CategoryLogs, engine.RiskSafe)
	oldF.LastUsed = time.Now().Add(-90 * 24 * time.Hour)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{cache, logs, oldF}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }

	if _, err := runErr("clean", "--yes", "--category", "logs", "--older-than", "30d"); err != nil {
		t.Fatal(err)
	}
	if !exists(cache.Path) || !exists(logs.Path) || exists(oldF.Path) {
		t.Errorf("cache kept=%v recent logs kept=%v old logs kept=%v; want true true false", exists(cache.Path), exists(logs.Path), exists(oldF.Path))
	}
}

func TestCleanPermanent(t *testing.T) {
	ce := useClean(t)
	a := ce.add(t, "a", engine.CategoryCache, engine.RiskSafe)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{a}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }

	out, err := runErr("clean", "--yes", "--permanent", "--no-color")
	if err != nil || exists(a.Path) {
		t.Fatalf("err=%v exists=%v", err, exists(a.Path))
	}
	if entries, _ := os.ReadDir(ce.trash); len(entries) != 0 {
		t.Error("permanent must bypass the trash")
	}
	if !strings.Contains(out, "Cleaned 1 item, freed 1.0 MB.") {
		t.Errorf("output:\n%s", out)
	}
}

func TestCleanFailureExitsNonZero(t *testing.T) {
	ce := useClean(t)
	a := ce.add(t, "a", engine.CategoryCache, engine.RiskSafe)
	useDetectors(t, fake{name: "x", fs: []engine.Finding{a}})
	env := engine.Env{Home: ce.home, OS: "linux", Getenv: func(string) string { return "" }}
	newEnv = func() (engine.Env, error) { return env, nil }
	base := newCleaner(env)
	newCleaner = func(engine.Env) *engine.Cleaner {
		base.Trash = func(string) (string, error) { return "", errors.New("no trash on this server") }
		return base
	}
	out, err := runErr("clean", "--yes", "--no-color")
	if exitCode(err) != ExitError || !strings.Contains(out, "no trash on this server") || !exists(a.Path) {
		t.Errorf("code=%d kept=%v out:\n%s", exitCode(err), exists(a.Path), out)
	}
}

func TestCleanBadFlags(t *testing.T) {
	useClean(t)
	useDetectors(t)
	for _, args := range [][]string{{"clean", "--category", "stuff"}, {"clean", "--older-than", "soon"}} {
		if _, err := runErr(args...); exitCode(err) != ExitError {
			t.Errorf("%v: code=%d", args, exitCode(err))
		}
	}
}

func TestCleanRejectsPathArguments(t *testing.T) {
	useClean(t)
	useDetectors(t)
	if _, err := runErr("clean", "/etc"); exitCode(err) != ExitError {
		t.Error("clean must never accept arbitrary paths")
	}
}
