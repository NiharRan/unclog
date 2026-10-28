package ui

import "strings"

// Bar renders frac (clamped to 0..1) as a fixed-width block bar, e.g. "████░░░░".
func Bar(frac float64, width int) string {
	frac = min(max(frac, 0), 1)
	n := int(frac*float64(width) + 0.5)
	if frac > 0 && n == 0 {
		n = 1 // a tiny share still shows
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

func percent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}
