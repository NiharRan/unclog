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
	if err := RenderScan(&b, fs, o); err != nil {
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
	if got := render(t, nil, TableOptions{Now: now}); got != "Nothing found.\n" {
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
	if err := WriteScanJSON(&b, sample(), []string{"pkg_caches: boom"}); err != nil {
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
	_ = WriteScanJSON(&b, nil, nil)
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
