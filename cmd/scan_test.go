package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

// useProcs replaces the process snapshot; useDetectors defaults it to "no AI processes".
func useProcs(t *testing.T, procs []engine.Proc) {
	t.Helper()
	old := procScan
	procScan = func(context.Context, engine.Env, *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
		return procs, engine.MemInfo{Total: 8 << 30, Used: 4 << 30, Available: 4 << 30, Pressure: "normal"}, nil
	}
	t.Cleanup(func() { procScan = old })
}

func aiProc(pid int32, tool string, rss uint64, orphan bool) engine.Proc {
	return engine.Proc{PID: pid, PPID: 1, Name: "node", ParentName: "launchd", Tool: tool, RSS: rss, Orphan: orphan, Confident: orphan,
		Cmdline: "node server.js --token hunter2", Created: time.Now().Add(-3 * time.Hour)}
}

func useDetectors(t *testing.T, ds ...engine.Detector) {
	t.Helper()
	useProcs(t, nil)
	oldEnv, oldDs := newEnv, detectorList
	newEnv = func() (engine.Env, error) {
		return engine.Env{Home: "/home/u", OS: "linux", Getenv: func(string) string { return "" }}, nil
	}
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

func TestScanWithRAM(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true), aiProc(11, "MCP servers", 50<<20, false), aiProc(12, "Cursor", 300<<20, false)})
	out, err := runErr("scan", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RAM  450.0 MB across 3 processes", "1 orphaned (100.0 MB)", "Found 5.0 MB reclaimable and 100.0 MB RAM in 1 orphaned process."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestScanOnlyRAM(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true)})
	out, err := runErr("scan", "--only", "ram", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "pip") || !strings.Contains(out, "Found 100.0 MB RAM in 1 orphaned process.") {
		t.Errorf("--only ram output:\n%s", out)
	}
	useProcs(t, nil)
	if _, err := runErr("scan", "--only", "ram"); exitCode(err) != ExitNothing {
		t.Errorf("no processes: code=%d; want %d", exitCode(err), ExitNothing)
	}
}

func TestScanOnlyDiskSkipsProcesses(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true)})
	out, err := runErr("scan", "--only", "disk", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"ram"`) {
		t.Errorf("--only disk should omit ram:\n%s", out)
	}
}

func TestScanJSONIncludesRAM(t *testing.T) {
	useDetectors(t)
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true)})
	out, err := runErr("scan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc ui.ScanJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.RAM == nil {
		t.Fatalf("err=%v doc=%+v", err, doc)
	}
	if g := doc.RAM.Groups; len(g) != 1 || g[0].Orphans != 1 || doc.RAM.System.Pressure != "normal" {
		t.Errorf("ram = %+v", doc.RAM)
	}
}

func TestScanProcessErrorIsWarning(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	procScan = func(context.Context, engine.Env, *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
		return nil, engine.MemInfo{}, errors.New("no permission")
	}
	out, err := runErr("scan", "--json")
	if err != nil || !strings.Contains(out, "processes: no permission") {
		t.Errorf("err=%v out=%s", err, out)
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
