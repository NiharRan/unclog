//go:build !darwin

package engine

import "os"

// renameNoReplace is a plain rename here.
// shortcut: can replace a file that appears after the existence check; only macOS restores
// call it (other systems restore through the system trash), add renameat2 if that changes.
func renameNoReplace(src, dst string) error { return os.Rename(src, dst) }
