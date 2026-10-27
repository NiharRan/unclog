package ui

import (
	"encoding/json"
	"io"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
)

// SchemaVersion is bumped on any breaking change to the JSON below; the paid app relies on it.
const SchemaVersion = 1

// ScanJSON is the stable `scan --json` document.
type ScanJSON struct {
	SchemaVersion int           `json:"schema_version"`
	TotalBytes    int64         `json:"total_bytes"`
	Findings      []FindingJSON `json:"findings"`
	Warnings      []string      `json:"warnings"`
}

type FindingJSON struct {
	Tool      string     `json:"tool"`
	Label     string     `json:"label"`
	Path      string     `json:"path"`
	SizeBytes int64      `json:"size_bytes"`
	Size      string     `json:"size"`
	Category  string     `json:"category"`
	Risk      string     `json:"risk"`
	LastUsed  *time.Time `json:"last_used"` // null when unknown
	Detector  string     `json:"detector"`
	CleanVia  []string   `json:"clean_via"` // null unless removal goes through a CLI

	ToolRunning bool `json:"tool_running"`
}

// WriteScanJSON writes the scan document; arrays are never null.
func WriteScanJSON(w io.Writer, fs []engine.Finding, warnings []string) error {
	doc := ScanJSON{
		SchemaVersion: SchemaVersion,
		TotalBytes:    int64(engine.Total(fs)),
		Findings:      []FindingJSON{},
		Warnings:      append([]string{}, warnings...),
	}
	for _, f := range fs {
		fj := FindingJSON{
			Tool: f.Tool, Label: f.Label, Path: f.Path, SizeBytes: int64(f.Size), Size: f.Size.String(),
			Category: string(f.Category), Risk: string(f.Risk), Detector: f.Detector, CleanVia: f.CleanVia, ToolRunning: f.ToolRunning,
		}
		if !f.LastUsed.IsZero() {
			t := f.LastUsed.UTC()
			fj.LastUsed = &t
		}
		doc.Findings = append(doc.Findings, fj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
