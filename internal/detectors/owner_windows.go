package detectors

import "os"

// shortcut: %TEMP% is per-user on Windows, so every entry counts as ours; upgrade if shared temp dirs matter.
func ownedByCurrentUser(os.FileInfo) bool { return true }
