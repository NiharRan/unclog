package update

import (
	"context"
	"errors"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.1.0", "v0.1.1", true}, {"0.1.0", "v0.2.0", true}, {"v0.9.0", "v0.10.0", true},
		{"v0.1.1", "v0.1.1", false}, {"v0.2.0", "v0.1.9", false},
		{"dev", "v9.9.9", false}, {"v0.1.0", "", false}, {"v0.1.0", "nightly", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestLatestCachesAndSurvivesFailure(t *testing.T) {
	env := engine.Env{Home: t.TempDir(), OS: "linux", Getenv: func(string) string { return "" }}
	old, calls, fail := Fetch, 0, false
	t.Cleanup(func() { Fetch = old })
	Fetch = func(context.Context) (string, error) {
		calls++
		if fail {
			return "", errors.New("offline")
		}
		return "v1.2.3", nil
	}
	if got := Latest(context.Background(), env, false); got != "v1.2.3" || calls != 1 {
		t.Fatalf("first = %q, calls %d", got, calls)
	}
	if got := Latest(context.Background(), env, false); got != "v1.2.3" || calls != 1 {
		t.Errorf("second must come from the cache: %q, calls %d", got, calls)
	}
	fail = true
	if got := Latest(context.Background(), env, true); got != "" || calls != 2 {
		t.Errorf("forced failing check = %q, calls %d", got, calls)
	}
}

func TestUpgradeCommand(t *testing.T) {
	for exe, want := range map[string]string{
		"/opt/homebrew/Caskroom/unclog/0.1.0/unclog":      "brew upgrade --cask unclog",
		`C:\Users\a\scoop\apps\unclog\current\unclog.exe`: "scoop update unclog",
		"/home/a/go/bin/unclog":                           "go install github.com/NiharRan/unclog@latest",
	} {
		if got := UpgradeCommand(exe); got != want {
			t.Errorf("UpgradeCommand(%q) = %q, want %q", exe, got, want)
		}
	}
}
