// Package trash moves paths to the OS trash.
package trash

import "github.com/Bios-Marcel/wastebasket/v2"

// viaSystem is the platform trash helper. On macOS it asks Finder, which plays the
// trash sound, so macOS tries a silent move first (see trash_darwin.go).
func viaSystem(path string) error { return wastebasket.Trash(path) }
