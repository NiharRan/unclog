// Package trash moves paths to the OS trash.
package trash

import (
	"math"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/Bios-Marcel/wastebasket/v2"
)

// viaSystem is the platform trash helper. On macOS it asks Finder, which plays the
// trash sound, so macOS tries a silent move first (see trash_darwin.go).
func viaSystem(path string) error { return wastebasket.Trash(path) }

// matchWindow is how far a trash deletion date may be from the logged time.
const matchWindow = 2 * time.Minute

// Locate finds an entry in the system trash (Linux, Windows) by its original path and
// the time it was trashed, so the log needs no trash location. Restoring never overwrites.
// shortcut: one trash query per entry; fine for a log of a few hundred, batch if it gets slow.
func Locate(e engine.TrashedEntry) (func() error, bool) {
	res, err := wastebasket.Query(wastebasket.QueryOptions{Search: []string{e.Orig}})
	if err != nil {
		return nil, false
	}
	var best wastebasket.TrashedFileInfo
	bestGap := math.MaxFloat64
	for _, m := range res.Matches {
		for _, t := range m {
			if gap := math.Abs(t.DeletionDate().Sub(e.Time).Seconds()); gap < bestGap && gap <= matchWindow.Seconds() {
				best, bestGap = t, gap
			}
		}
	}
	if best == nil {
		return nil, false
	}
	return func() error { return best.Restore(false) }, true
}
