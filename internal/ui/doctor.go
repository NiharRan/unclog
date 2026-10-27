package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
)

// RenderDoctor prints the ranked problems, each with its fix on the next line.
func RenderDoctor(w io.Writer, problems []engine.Problem, color bool) error {
	st := newStyles(color)
	var b strings.Builder
	if len(problems) == 0 {
		b.WriteString("No problems found.\n")
	} else {
		fmt.Fprintf(&b, "%s\n\n", st.bold.Render(fmt.Sprintf("Top %s", plural(len(problems), "problem"))))
		for i, p := range problems {
			fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, p.Title, st.dim.Render("fix: ")+st.safe.Render(p.Fix))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// DoctorJSON is the stable `doctor --json` document.
type DoctorJSON struct {
	SchemaVersion int           `json:"schema_version"`
	Problems      []ProblemJSON `json:"problems"`
	Memory        SystemJSON    `json:"memory"`
	Disk          DiskJSON      `json:"disk"`
	Warnings      []string      `json:"warnings"`
}

type ProblemJSON struct {
	Rank  int    `json:"rank"`
	Kind  string `json:"kind"` // disk, ram or system
	Title string `json:"title"`
	Bytes uint64 `json:"bytes"`
	Fix   string `json:"fix"`
}

type DiskJSON struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// WriteDoctorJSON writes the doctor document; arrays are never null.
func WriteDoctorJSON(w io.Writer, problems []engine.Problem, mem engine.MemInfo, dsk engine.DiskInfo, warnings []string) error {
	doc := DoctorJSON{
		SchemaVersion: SchemaVersion,
		Problems:      []ProblemJSON{},
		Memory:        newSystemJSON(mem),
		Disk:          DiskJSON{dsk.Path, dsk.Total, dsk.Free},
		Warnings:      append([]string{}, warnings...),
	}
	for i, p := range problems {
		doc.Problems = append(doc.Problems, ProblemJSON{i + 1, p.Kind, p.Title, p.Bytes, p.Fix})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
