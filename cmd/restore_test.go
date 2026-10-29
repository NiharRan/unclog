package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/safety"
)

func TestRestoreAllPutsFileBack(t *testing.T) {
	home := t.TempDir()
	env := engine.Env{Home: home, OS: "darwin", Getenv: func(string) string { return "" }}
	old := newEnv
	newEnv = func() (engine.Env, error) { return env, nil }
	t.Cleanup(func() { newEnv = old })

	orig, dest := filepath.Join(home, ".cache", "foo"), filepath.Join(home, ".Trash", "foo")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := safety.NewHistory(env).Log("trash", orig, 1, "trashed"+engine.TrashedTo+dest); err != nil {
		t.Fatal(err)
	}

	out := run(t, "restore", "--all", "--dry-run")
	if !strings.Contains(out, "would restore "+orig) || exists(orig) {
		t.Fatalf("dry run: %q, orig exists=%v", out, exists(orig))
	}
	if out = run(t, "restore", "--all"); !strings.Contains(out, "restored "+orig) || !exists(orig) || exists(dest) {
		t.Fatalf("restore: %q", out)
	}
	if _, err := runErr("restore", "--all"); err == nil {
		t.Error("second restore should report nothing to restore")
	}
}
