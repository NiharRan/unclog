package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSizeFormatParse(t *testing.T) {
	for _, in := range []string{"1.5 GB", "100.0 MB", "512 B", "2.0 TB"} {
		s, err := ParseSize(in)
		if err != nil {
			t.Fatalf("ParseSize(%q): %v", in, err)
		}
		if got := s.String(); got != in {
			t.Errorf("round trip %q -> %q", in, got)
		}
	}
	for in, want := range map[string]Size{"100MB": 100 << 20, "1kb": 1024, "42": 42, " 2 GB ": 2 << 30} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MB", "-1MB", "abc", "10XB", "1.2.3GB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) should fail", in)
		}
	}
}

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirSize(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 100)
	write(t, filepath.Join(root, "d", "b"), 200)
	write(t, filepath.Join(root, "d", "e", "c"), 300)

	got, err := DirSize(context.Background(), root)
	if err != nil || got != 600 {
		t.Fatalf("DirSize = %d, %v; want 600", got, err)
	}
	if lu, err := LastUsed(context.Background(), root); err != nil || lu.IsZero() {
		t.Fatalf("LastUsed = %v, %v", lu, err)
	}
}

func TestDirSizeIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 100)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "big"), 5000)
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if got, err := DirSize(context.Background(), root); err != nil || got != 100 {
		t.Fatalf("DirSize = %d, %v; want 100", got, err)
	}
	// A symlink as the root itself is not followed either.
	if got, _ := DirSize(context.Background(), filepath.Join(root, "out")); got != 0 {
		t.Fatalf("symlink root size = %d; want 0", got)
	}
}

func TestDirSizeUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0 not enforced here")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 100)
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "x"), 999)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if got, err := DirSize(context.Background(), root); err != nil || got != 100 {
		t.Fatalf("DirSize = %d, %v; want 100", got, err)
	}
}

func TestDirSizeCancelled(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DirSize(ctx, root); err == nil {
		t.Fatal("want error from cancelled context")
	}
}

func TestDirSizeMissing(t *testing.T) {
	if _, err := DirSize(context.Background(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want error for missing path")
	}
}
