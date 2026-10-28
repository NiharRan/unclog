package ui

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
)

var update = flag.Bool("update", false, "rewrite golden files")

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func sample() []engine.Finding {
	return []engine.Finding{
		{Tool: "pnpm", Label: "pnpm store", Path: "/h/pnpm", Size: 8 << 30, Category: engine.CategoryCache, Risk: engine.RiskSafe, LastUsed: now.Add(-48 * time.Hour)},
		{Tool: "Hugging Face", Label: "org/alpha", Path: "/h/hf/a", Size: 3 << 30, Category: engine.CategoryModel, Risk: engine.RiskCareful, LastUsed: now.Add(-100 * 24 * time.Hour)},
		{Tool: "Hugging Face", Label: "dataset org/data", Path: "/h/hf/d", Size: 500 << 20, Category: engine.CategoryModel, Risk: engine.RiskCareful},
		{Tool: "Temp files", Label: "a-very-long-temporary-directory-name-that-must-be-truncated", Path: "/t/x", Size: 2 << 20, Category: engine.CategoryTmp, Risk: engine.RiskSafe, LastUsed: now.Add(-30 * time.Minute)},
		{Tool: "Ollama", Label: "llama3:latest", Size: 4 << 30, Category: engine.CategoryModel, Risk: engine.RiskCareful, CleanVia: []string{"ollama", "rm", "llama3:latest"}},
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func render(t *testing.T, fs []engine.Finding, o TableOptions) string {
	t.Helper()
	var b bytes.Buffer
	if err := RenderScan(&b, fs, nil, o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRenderScanGolden(t *testing.T) {
	got := render(t, sample(), TableOptions{Now: now})
	golden(t, "scan.golden", got)
	if strings.Contains(got, "\x1b") {
		t.Error("color-off output contains ANSI codes")
	}
}

func TestRenderScanQuietHidesFooter(t *testing.T) {
	got := render(t, sample(), TableOptions{Now: now, Quiet: true})
	if strings.Contains(got, "waitlist") {
		t.Error("quiet output still has the waitlist footer")
	}
	if !strings.Contains(got, "Found 15.5 GB reclaimable.") {
		t.Errorf("missing summary line:\n%s", got)
	}
}

func TestRenderScanEmpty(t *testing.T) {
	if got := render(t, nil, TableOptions{Now: now}); got != "Nothing found. Your AI tools are not using noticeable disk space or memory.\n" {
		t.Errorf("got %q", got)
	}
}

func TestRenderScanColorEmitsANSI(t *testing.T) {
	// lipgloss picks its colour profile from the terminal; force one so the test is deterministic.
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	_ = render(t, sample(), TableOptions{Color: true, Now: now}) // must not panic
}

func TestWriteScanJSON(t *testing.T) {
	var b bytes.Buffer
	if err := WriteScanJSON(&b, sample(), nil, []string{"pkg_caches: boom"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "\x1b") {
		t.Error("JSON contains ANSI codes")
	}
	var doc ScanJSON
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != SchemaVersion || len(doc.Findings) != 5 || doc.Warnings[0] != "pkg_caches: boom" {
		t.Errorf("doc = %+v", doc)
	}
	if doc.TotalBytes != int64(engine.Total(sample())) {
		t.Errorf("total = %d", doc.TotalBytes)
	}
	if doc.Findings[2].LastUsed != nil {
		t.Error("zero LastUsed must be null")
	}
	if got := doc.Findings[4].CleanVia; len(got) != 3 {
		t.Errorf("clean_via = %v", got)
	}
}

func TestWriteScanJSONEmptyArraysNotNull(t *testing.T) {
	var b bytes.Buffer
	_ = WriteScanJSON(&b, nil, nil, nil)
	for _, want := range []string{`"findings": []`, `"warnings": []`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %s in\n%s", want, b.String())
		}
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "just now", 25 * time.Hour: "1 day ago", 5 * time.Minute: "5 min ago", 3 * time.Hour: "3 hours ago",
		72 * time.Hour: "3 days ago", 90 * 24 * time.Hour: "3 months ago", 800 * 24 * time.Hour: "2 years ago",
	} {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(%v) = %q; want %q", d, got, want)
		}
	}
	if ago(time.Time{}, now) != "-" {
		t.Error("zero time should render -")
	}
}

func sampleProcs() []engine.Proc {
	mk := func(pid, ppid int32, tool, parent string, rss uint64, cpu float64, age time.Duration, orphan, confident bool) engine.Proc {
		return engine.Proc{PID: pid, PPID: ppid, ParentName: parent, Tool: tool, RSS: rss, CPU: cpu, Created: now.Add(-age), Orphan: orphan, Confident: confident}
	}
	return []engine.Proc{
		mk(101, 1, "MCP servers", "launchd", 800<<20, 0, 50*time.Hour, true, true),
		mk(102, 1, "MCP servers", "launchd", 300<<20, 0.1, 3*time.Hour, true, false),
		mk(103, 77, "MCP servers", "Cursor", 120<<20, 2.5, 90*time.Minute, false, false),
		mk(200, 1, "Cursor", "launchd", 1500<<20, 12.3, 5*time.Hour, false, false),
		mk(300, 1, "Ollama", "launchd", 5<<30, 0, 26*time.Hour, false, false),
	}
}

var sampleMem = engine.MemInfo{Total: 16 << 30, Used: 11 << 30, Available: 5 << 30, Pressure: "warning"}

func TestRenderScanWithRAMGolden(t *testing.T) {
	var b bytes.Buffer
	if err := RenderScan(&b, sample(), &RAMReport{Procs: sampleProcs(), Mem: sampleMem}, TableOptions{Now: now}); err != nil {
		t.Fatal(err)
	}
	golden(t, "scan_ram.golden", b.String())
}

func TestRenderScanRAMOnlySummary(t *testing.T) {
	var b bytes.Buffer
	_ = RenderScan(&b, nil, &RAMReport{Procs: sampleProcs()[:1], Mem: sampleMem}, TableOptions{Now: now, Quiet: true})
	if !strings.Contains(b.String(), "Found 800.0 MB RAM in 1 orphaned process.") {
		t.Errorf("summary:\n%s", b.String())
	}
	b.Reset()
	_ = RenderScan(&b, nil, &RAMReport{Procs: sampleProcs()[3:4], Mem: sampleMem}, TableOptions{Now: now, Quiet: true})
	if !strings.Contains(b.String(), "No orphaned processes.") {
		t.Errorf("summary:\n%s", b.String())
	}
}

func TestRenderPSGolden(t *testing.T) {
	var b bytes.Buffer
	if err := RenderPS(&b, sampleProcs(), sampleMem, TableOptions{Now: now}); err != nil {
		t.Fatal(err)
	}
	golden(t, "ps.golden", b.String())
	if strings.Contains(b.String(), "\x1b") {
		t.Error("color-off output contains ANSI codes")
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{1: "1 process", 2: "2 processes"} {
		if got := plural(n, "process"); got != want {
			t.Errorf("plural(%d) = %q; want %q", n, got, want)
		}
	}
}

func TestRenderDoctor(t *testing.T) {
	ps := []engine.Problem{
		{Kind: "disk", Title: "pnpm: 8.0 GB reclaimable in 1 item", Fix: "unclog clean --category cache"},
		{Kind: "ram", Title: "2 orphaned processes using 1.0 GB of RAM", Fix: "unclog kill --orphans"},
	}
	var b bytes.Buffer
	if err := RenderDoctor(&b, ps, false); err != nil {
		t.Fatal(err)
	}
	golden(t, "doctor.golden", b.String())
	b.Reset()
	_ = RenderDoctor(&b, nil, false)
	if b.String() != "✔ No problems found. Nothing is wasting noticeable space or memory.\n" {
		t.Errorf("healthy output %q", b.String())
	}
}
