//go:build !darwin

package trash

// Move sends path to the OS trash.
func Move(path string) error { return viaSystem(path) }
