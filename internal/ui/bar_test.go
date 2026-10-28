package ui

import "testing"

func TestBar(t *testing.T) {
	for _, c := range []struct {
		frac  float64
		width int
		want  string
	}{
		{0, 4, "░░░░"}, {0.5, 4, "██░░"}, {1, 4, "████"}, {2, 4, "████"}, {-1, 4, "░░░░"}, {0.01, 4, "█░░░"},
	} {
		if got := Bar(c.frac, c.width); got != c.want {
			t.Errorf("Bar(%v,%d)=%q want %q", c.frac, c.width, got, c.want)
		}
	}
}
