package engine_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
)

type noLog struct{}

func (noLog) Log(string, string, engine.Size, string) error { return nil }

func TestListAndRestoreTrashed(t *testing.T) {
	dir := t.TempDir()
	trashDir, orig := filepath.Join(dir, ".Trash"), filepath.Join(dir, "cache", "x")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(trashDir, "x")
	if err := os.WriteFile(dest, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := func(action, target, outcome string) string {
		return "2026-01-02T03:04:05Z\t" + action + "\t" + strconv.Quote(target) + "\t42\t" + strconv.Quote(outcome) + "\n"
	}
	log := filepath.Join(dir, "history.log")
	body := line("trash", orig, "started") + // never finished: skipped
		line("trash", orig, "trashed: "+dest) +
		line("trash", orig, "trashed: "+filepath.Join(dir, "elsewhere", "x")) + // outside the Trash: skipped
		line("trash", orig, "trashed: "+filepath.Join(trashDir, "gone")) // not on disk: skipped
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := engine.ListTrashed(log, engine.InTrash(trashDir))
	if err != nil || len(got) != 1 || got[0].Dest != dest || got[0].Orig != orig || got[0].Size != 42 {
		t.Fatalf("ListTrashed = %+v, %v", got, err)
	}

	r := &engine.Restorer{Log: noLog{}, Denied: func(string) string { return "" }}
	if err := r.Restore(got[0]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(orig); string(b) != "hi" {
		t.Errorf("restored file content = %q", b)
	}
	if err := os.WriteFile(dest, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(got[0]); err == nil {
		t.Error("restore over an existing file must fail")
	}
	r.Denied = func(string) string { return "protected" }
	_ = os.Remove(orig)
	if err := r.Restore(got[0]); err == nil {
		t.Error("restore of a denied path must fail")
	}
}

func TestListTrashedWithoutRecordedLocation(t *testing.T) {
	log := filepath.Join(t.TempDir(), "history.log")
	body := "2026-01-02T03:04:05Z\ttrash\t" + strconv.Quote("/a/b") + "\t7\t" + strconv.Quote("trashed") + "\n"
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	locate := func(e engine.TrashedEntry) (func() error, bool) {
		return func() error { called = true; return nil }, e.Orig == "/a/b" && e.Dest == ""
	}
	got, err := engine.ListTrashed(log, locate)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListTrashed = %+v, %v", got, err)
	}
	if err := got[0].Undo(); err != nil || !called {
		t.Errorf("undo not wired: %v", err)
	}
}

func TestRestoreRefusesFolderTurnedSymlink(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir()) // macOS temp dirs sit behind a /var symlink
	protected, orig := filepath.Join(dir, "protected"), filepath.Join(dir, "proj", "x")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, filepath.Join(dir, "proj")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "trashed")
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &engine.Restorer{Log: noLog{}, Denied: func(p string) string {
		if filepath.Dir(p) == protected {
			return "protected"
		}
		return ""
	}}
	e := engine.TrashedEntry{Orig: orig, Dest: dest, Undo: func() error { return os.Rename(dest, orig) }}
	if err := r.Restore(e); err == nil {
		t.Fatal("restore through a symlink into a protected folder must fail")
	}
	if _, err := os.Lstat(filepath.Join(protected, "x")); err == nil {
		t.Error("file landed in the protected folder")
	}
}

func TestInTrashUndoNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	trashDir := filepath.Join(dir, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest, orig := filepath.Join(trashDir, "x"), filepath.Join(dir, "x")
	for p, c := range map[string]string{dest: "trashed", orig: "newer"} {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	undo, ok := engine.InTrash(trashDir)(engine.TrashedEntry{Orig: orig, Dest: dest})
	if !ok {
		t.Fatal("entry not located")
	}
	if runtime.GOOS == "darwin" && undo() == nil {
		t.Error("undo replaced an existing file")
	}
	if b, _ := os.ReadFile(orig); runtime.GOOS == "darwin" && string(b) != "newer" {
		t.Errorf("existing file changed to %q", b)
	}
}
