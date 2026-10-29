//go:build darwin

package engine

import "golang.org/x/sys/unix"

// renameNoReplace moves src to dst and fails if dst appears in the meantime.
func renameNoReplace(src, dst string) error { return unix.RenamexNp(src, dst, unix.RENAME_EXCL) }
