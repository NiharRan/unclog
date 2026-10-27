package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/version"
)

func run(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.String()
}

func TestVersionContainsInjectedVersion(t *testing.T) {
	old := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = old })

	if got := run(t, "version"); !strings.Contains(got, "1.2.3") {
		t.Fatalf("version output %q missing 1.2.3", got)
	}
}

func TestHelp(t *testing.T) {
	if got := run(t, "--help"); !strings.Contains(got, "unclog") {
		t.Fatalf("help output %q missing name", got)
	}
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled() {
		t.Fatal("color enabled despite NO_COLOR")
	}
}
