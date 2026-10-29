//go:build !darwin

package trash

// Move sends path to the OS trash. The trash location is unknown here, so it returns "".
func Move(path string) (string, error) { return "", viaSystem(path) }
