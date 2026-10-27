package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
)

type fake struct {
	name string
	fs   []engine.Finding
	err  error
}

func (f fake) Name() string { return f.name }
func (f fake) Scan(context.Context, engine.Env) ([]engine.Finding, error) {
	return f.fs, f.err
}

func useDetectors(t *testing.T, ds ...engine.Detector) {
	t.Helper()
	oldEnv, oldDs := newEnv, detectorList
	newEnv = func() (engine.Env, error) { return engine.Env{Home: "/home/u", OS: "linux", Getenv: func(string) string { return "" }}, nil }
	detectorList = func() []engine.Detector { return ds }
	t.Cleanup(func() { newEnv, detectorList = oldEnv, oldDs })
}

func finding(path string, size engine.Size) engine.Finding {
	return engine.Finding{Tool: "T", Label: path, Path: path, Size: size, Category: engine.CategoryCache, Risk: engine.RiskSafe}
}

func exitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	if err != nil {
		return ExitError
	}
	return ExitOK
}

func TestScanTable(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	out, err := runErr("scan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Found 5.0 MB reclaimable.") || !strings.Contains(out, "waitlist") {
		t.Errorf("output:\n%s", out)
	}
	if out, _ := runErr("scan", "--quiet"); strings.Contains(out, "waitlist") {
		t.Error("--quiet should hide the footer")
	}
}

func TestScanJSON(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	out, err := runErr("scan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc ui.ScanJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not pure JSON: %v\n%s", err, out)
	}
	if len(doc.Findings) != 1 || doc.SchemaVersion != ui.SchemaVersion {
		t.Errorf("doc = %+v", doc)
	}
}

func TestScanMinSize(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/big", 200<<20), finding("/home/u/.cache/small", 1<<20)}})
	out, err := runErr("scan", "--json", "--min-size", "100MB")
	if err != nil {
		t.Fatal(err)
	}
	var doc ui.ScanJSON
	_ = json.Unmarshal([]byte(out), &doc)
	if len(doc.Findings) != 1 || doc.Findings[0].Path != "/home/u/.cache/big" {
		t.Errorf("findings = %+v", doc.Findings)
	}
}

func TestScanNothingFoundExitCode(t *testing.T) {
	useDetectors(t, fake{name: "a"})
	out, err := runErr("scan")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "Nothing found.") {
		t.Errorf("code=%d out=%q", exitCode(err), out)
	}
	// JSON is still emitted for scripts, with the same exit code.
	out, err = runErr("scan", "--json")
	if exitCode(err) != ExitNothing || !strings.Contains(out, `"findings": []`) {
		t.Errorf("json: code=%d out=%q", exitCode(err), out)
	}
}

func TestScanOnlyRAMNotAvailableYet(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	out, err := runErr("scan", "--only", "ram")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "not available yet") {
		t.Errorf("code=%d out=%q", exitCode(err), out)
	}
}

func TestScanOnlyDisk(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	if _, err := runErr("scan", "--only", "disk"); err != nil {
		t.Fatal(err)
	}
}

func TestScanBadFlags(t *testing.T) {
	useDetectors(t, fake{name: "a"})
	for _, args := range [][]string{{"scan", "--only", "gpu"}, {"scan", "--min-size", "lots"}} {
		if _, err := runErr(args...); exitCode(err) != ExitError {
			t.Errorf("%v: code=%d; want %d", args, exitCode(err), ExitError)
		}
	}
}

func TestScanDetectorErrorIsWarning(t *testing.T) {
	useDetectors(t,
		fake{name: "bad", err: errors.New("boom")},
		fake{name: "good", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}},
	)
	out, err := runErr("scan", "--json")
	if err != nil {
		t.Fatalf("a failing detector must not fail the scan: %v", err)
	}
	if !strings.Contains(out, "bad: boom") {
		t.Errorf("warning missing from JSON:\n%s", out)
	}
}

func TestScanDropsDeniedPaths(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{
		finding("/home/u/Documents/x", 9<<20),
		finding("/home/u/.cache/pip", 1<<20),
		{Tool: "Docker", Label: "images", Size: 3 << 20, CleanVia: []string{"docker", "image", "prune"}},
	}})
	out, _ := runErr("scan", "--json")
	var doc ui.ScanJSON
	_ = json.Unmarshal([]byte(out), &doc)
	if len(doc.Findings) != 2 {
		t.Fatalf("findings = %+v", doc.Findings)
	}
	for _, f := range doc.Findings {
		if strings.Contains(f.Path, "Documents") {
			t.Error("denied path leaked into the scan")
		}
	}
}
