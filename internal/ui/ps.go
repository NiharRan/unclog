package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
)

const cmdMaxLen = 200

func orphanLabel(p engine.Proc) string {
	switch {
	case !p.Orphan:
		return "-"
	case p.Confident:
		return "yes"
	}
	return "likely"
}

func uptime(created, now time.Time) string {
	if created.IsZero() {
		return "-"
	}
	d := now.Sub(created)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// sortProcs orders by tool group (largest first), then by memory.
func sortProcs(procs []engine.Proc) []engine.Proc {
	rank := map[string]int{}
	for i, g := range engine.GroupByTool(procs) {
		rank[g.Tool] = i
	}
	out := append([]engine.Proc(nil), procs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return rank[out[i].Tool] < rank[out[j].Tool]
		}
		return out[i].RSS > out[j].RSS
	})
	return out
}

// RenderPS writes the live process table.
func RenderPS(w io.Writer, procs []engine.Proc, mem engine.MemInfo, o TableOptions) error {
	st := newStyles(o.Color)
	var b strings.Builder
	if len(procs) == 0 {
		b.WriteString("No AI-related processes found. Nothing from your AI tools is running right now.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	procs = sortProcs(procs)
	rows := make([][]string, len(procs))
	widths := []int{len("TOOL"), len("PID"), len("RAM"), len("CPU"), len("UPTIME"), len("PARENT"), len("ORPHAN")}
	for i, p := range procs {
		parent := fmt.Sprintf("%s(%d)", p.ParentName, p.PPID)
		if p.ParentName == "" {
			parent = fmt.Sprint(p.PPID)
		}
		rows[i] = []string{p.Tool, fmt.Sprint(p.PID), engine.Size(p.RSS).String(), fmt.Sprintf("%.1f%%", p.CPU), uptime(p.Created, o.Now), truncate(parent), orphanLabel(p)}
		for c, v := range rows[i] {
			widths[c] = max(widths[c], len([]rune(v)))
		}
	}

	line := func(cells []string, style func(i int, s string) string) {
		for c, v := range cells {
			if c > 0 {
				b.WriteString("  ")
			}
			cell := v // last column is not padded: no trailing spaces
			switch {
			case c == 1 || c == 2 || c == 3: // numbers right-aligned
				cell = padLeft(v, widths[c])
			case c < len(cells)-1:
				cell = pad(v, widths[c])
			}
			b.WriteString(style(c, cell))
		}
		b.WriteString("\n")
	}
	line([]string{"TOOL", "PID", "RAM", "CPU", "UPTIME", "PARENT", "ORPHAN"}, func(_ int, s string) string { return st.bold.Render(s) })
	for i, r := range rows {
		line(r, func(c int, s string) string {
			if c == 6 && procs[i].Orphan {
				return st.careful.Render(s)
			}
			return s
		})
	}
	if mem.Total > 0 {
		fmt.Fprintf(&b, "\n%s\n", st.dim.Render(fmt.Sprintf("system: %s used of %s, pressure %s", engine.Size(mem.Used), engine.Size(mem.Total), mem.Pressure)))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// PSJSON is the stable `ps --json` document. Command lines are masked and truncated.
type PSJSON struct {
	SchemaVersion int         `json:"schema_version"`
	System        SystemJSON  `json:"system"`
	Groups        []GroupJSON `json:"groups"`
	Processes     []ProcJSON  `json:"processes"`
}

type SystemJSON struct {
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	Pressure       string `json:"pressure"`
}

type GroupJSON struct {
	Tool           string `json:"tool"`
	Processes      int    `json:"processes"`
	RSSBytes       uint64 `json:"rss_bytes"`
	Orphans        int    `json:"orphans"`
	OrphanRSSBytes uint64 `json:"orphan_rss_bytes"`
}

type ProcJSON struct {
	Tool            string  `json:"tool"`
	PID             int32   `json:"pid"`
	PPID            int32   `json:"ppid"`
	Parent          string  `json:"parent"`
	Name            string  `json:"name"`
	Cmd             string  `json:"cmd"`
	RSSBytes        uint64  `json:"rss_bytes"`
	CPUPercent      float64 `json:"cpu_percent"`
	UptimeSeconds   int64   `json:"uptime_seconds"`
	Orphan          bool    `json:"orphan"`
	OrphanConfident bool    `json:"orphan_confident"` // false: idleness estimated from lifetime CPU
}

func newSystemJSON(m engine.MemInfo) SystemJSON {
	return SystemJSON{m.Total, m.Used, m.Available, m.Pressure}
}

func newGroupsJSON(procs []engine.Proc) []GroupJSON {
	gs := []GroupJSON{}
	for _, g := range engine.GroupByTool(procs) {
		gs = append(gs, GroupJSON{g.Tool, g.Count, g.RSS, g.Orphans, g.OrphanRSS})
	}
	return gs
}

// WritePSJSON writes the ps document; arrays are never null.
func WritePSJSON(w io.Writer, procs []engine.Proc, mem engine.MemInfo, now time.Time) error {
	doc := PSJSON{SchemaVersion: SchemaVersion, System: newSystemJSON(mem), Groups: newGroupsJSON(procs), Processes: []ProcJSON{}}
	for _, p := range sortProcs(procs) {
		var up int64
		if !p.Created.IsZero() {
			up = int64(now.Sub(p.Created).Seconds())
		}
		doc.Processes = append(doc.Processes, ProcJSON{
			Tool: p.Tool, PID: p.PID, PPID: p.PPID, Parent: p.ParentName, Name: p.Name,
			Cmd: engine.MaskCmdline(p.Cmdline, cmdMaxLen), RSSBytes: p.RSS, CPUPercent: p.CPU,
			UptimeSeconds: up, Orphan: p.Orphan, OrphanConfident: p.Orphan && p.Confident,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
