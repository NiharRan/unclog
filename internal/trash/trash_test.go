//go:build darwin

package trash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveRenamesIntoTrashAndKeepsDuplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := t.TempDir()
	for i := 0; i < 2; i++ {
		p := filepath.Join(src, "cache")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Move(p); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("source still exists after move %d", i)
		}
	}
	// Same second is fine for the first name; a second move must not overwrite the first.
	entries, _ := os.ReadDir(filepath.Join(home, ".Trash"))
	if len(entries) != 2 {
		t.Errorf("want 2 entries in Trash, got %d", len(entries))
	}
}
