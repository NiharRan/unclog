package engine_test

import (
	"os"
	"path/filepath"
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
