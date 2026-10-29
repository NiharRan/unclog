//go:build darwin

package trash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Move renames path into ~/.Trash so Finder is never involved and nothing plays a sound.
// It returns the new location, or "" when the system trash was used and the location is unknown.
// shortcut: no "Put Back" metadata, and a path on another volume falls back to Finder
// (which plays the sound); upgrade to NSFileManager trashItem if that matters.
func Move(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", viaSystem(path)
	}
	dir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", viaSystem(path)
	}
	dst := filepath.Join(dir, filepath.Base(path))
	if _, err := os.Lstat(dst); err == nil { // same name already in the Trash: add a time like Finder does
		ext := filepath.Ext(dst)
		base := fmt.Sprintf("%s %s", dst[:len(dst)-len(ext)], time.Now().Format("15.04.05"))
		dst = base + ext
		for n := 2; ; n++ { // never let Rename overwrite an existing entry
			if _, err := os.Lstat(dst); err != nil {
				break
			}
			dst = fmt.Sprintf("%s %d%s", base, n, ext)
		}
	}
	if err := os.Rename(path, dst); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return "", viaSystem(path)
		}
		return "", err
	}
	return dst, nil
}
