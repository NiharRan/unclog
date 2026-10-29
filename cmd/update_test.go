package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/update"
	"github.com/NiharRan/unclog/internal/version"
)

// useUpdate points the commands at a temp home whose config sets update_check and fakes the release lookup.
func useUpdate(t *testing.T, optIn bool, latest string) *int {
	t.Helper()
	home := t.TempDir()
	env := engine.Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	if optIn {
		cfg := filepath.Join(home, ".config", "unclog", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, []byte("update_check: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	oldEnv, oldFetch, oldTTY, oldVer, oldExe := newEnv, update.Fetch, stderrIsTerminal, version.Version, exePath
	newEnv = func() (engine.Env, error) { return env, nil }
	update.Fetch = func(context.Context) (string, error) { calls++; return latest, nil }
	stderrIsTerminal = func() bool { return true }
	exePath = func() string { return "/home/a/go/bin/unclog" }
	version.Version = "v0.1.0"
	t.Cleanup(func() {
		newEnv, update.Fetch, stderrIsTerminal, version.Version, exePath = oldEnv, oldFetch, oldTTY, oldVer, oldExe
	})
	return &calls
}

func TestUpdateHintOnlyWhenOptedIn(t *testing.T) {
	calls := useUpdate(t, false, "v0.2.0")
	if out := run(t, "config", "path"); strings.Contains(out, "new unclog") || *calls != 0 {
		t.Fatalf("not opted in, but output %q and %d network calls", out, *calls)
	}
	calls = useUpdate(t, true, "v0.2.0")
	out := run(t, "config", "path")
	if !strings.Contains(out, "A new unclog v0.2.0 is available (you have v0.1.0). Update: go install github.com/NiharRan/unclog@latest") {
		t.Errorf("missing update hint: %q", out)
	}
	run(t, "config", "path")
	if *calls != 1 {
		t.Errorf("want 1 network call across two runs, got %d", *calls)
	}
}

func TestUpdateHintSilentWhenUpToDateOrPiped(t *testing.T) {
	useUpdate(t, true, "v0.1.0")
	if out := run(t, "config", "path"); strings.Contains(out, "new unclog") {
		t.Errorf("up to date, got hint: %q", out)
	}
	useUpdate(t, true, "v0.2.0")
	stderrIsTerminal = func() bool { return false }
	if out := run(t, "config", "path"); strings.Contains(out, "new unclog") {
		t.Errorf("piped, got hint: %q", out)
	}
}

func TestVersionCheckIgnoresOptIn(t *testing.T) {
	useUpdate(t, false, "v0.2.0")
	if out := run(t, "version", "--check"); !strings.Contains(out, "A new unclog v0.2.0 is available") {
		t.Errorf("version --check: %q", out)
	}
}
