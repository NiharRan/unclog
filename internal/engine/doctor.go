package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskInfo is free space on the volume holding the home directory.
type DiskInfo struct {
	Path        string
	Total, Free uint64
}

func ReadDisk(ctx context.Context, path string) DiskInfo {
	u, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return DiskInfo{}
	}
	return DiskInfo{Path: path, Total: u.Total, Free: u.Free}
}

// Problem is one ranked finding of `doctor`.
type Problem struct {
	Kind  string // disk, ram or system
	Title string
	Bytes uint64 // reclaimable disk or RAM behind the problem, 0 for system problems
	Fix   string // one-line command
	score float64
}

const (
	doctorTop       = 5
	minDiskProblem  = 100 << 20
	ramWeight       = 5.0     // a byte of RAM is scarcer than a byte of disk
	scoreWarning    = 5 << 30 // below a large cleanup, above small ones
	scoreCritical   = 1 << 40
	lowDiskAbsolute = 10 << 30
	lowDiskPercent  = 10
)

// Diagnose ranks problems by impact and returns the top five. Disk problems are one per
// tool (bytes reclaimable), RAM is the orphaned processes, and memory pressure or a nearly
// full disk outrank both. A healthy machine returns nothing.
func Diagnose(fs []Finding, procs []Proc, mem MemInfo, dsk DiskInfo) []Problem {
	var ps []Problem

	type group struct {
		total Size
		items int
		byCat map[Category]Size
	}
	groups := map[string]*group{}
	for _, f := range fs {
		g := groups[f.Tool]
		if g == nil {
			g = &group{byCat: map[Category]Size{}}
			groups[f.Tool] = g
		}
		g.total += f.Size
		g.items++
		g.byCat[f.Category] += f.Size
	}
	for tool, g := range groups {
		if g.total < minDiskProblem {
			continue
		}
		cat, best := CategoryCache, Size(-1)
		for c, s := range g.byCat {
			if s > best || (s == best && c < cat) {
				cat, best = c, s
			}
		}
		fix := "unclog clean --category " + string(cat)
		if cat == CategoryModel {
			fix = "unclog clean --category models"
		}
		ps = append(ps, Problem{
			Kind: "disk", Title: fmt.Sprintf("%s: %s reclaimable in %s", tool, g.total, plural(g.items, "item")),
			Bytes: uint64(g.total), Fix: fix, score: float64(g.total),
		})
	}

	if n, rss := OrphanTotals(procs); n > 0 {
		ps = append(ps, Problem{
			Kind: "ram", Title: fmt.Sprintf("%s using %s of RAM", plural(n, "orphaned process"), Size(rss)),
			Bytes: rss, Fix: "unclog kill --orphans", score: float64(rss) * ramWeight,
		})
	}

	switch mem.Pressure {
	case "warning", "critical":
		score := float64(scoreWarning)
		if mem.Pressure == "critical" {
			score = scoreCritical
		}
		ps = append(ps, Problem{
			Kind: "system", Title: fmt.Sprintf("Memory pressure is %s: %s of %s in use", mem.Pressure, Size(mem.Used), Size(mem.Total)),
			Fix: "unclog ps", score: score,
		})
	}

	if dsk.Total > 0 {
		pct := float64(dsk.Free) / float64(dsk.Total) * 100
		if dsk.Free < lowDiskAbsolute || pct < lowDiskPercent {
			score := float64(scoreWarning)
			if dsk.Free < 2<<30 || pct < 3 {
				score = scoreCritical
			}
			ps = append(ps, Problem{
				Kind: "system", Title: fmt.Sprintf("Only %s free on the disk holding %s (%.0f%%)", Size(dsk.Free), dsk.Path, pct),
				Fix: "unclog clean", score: score,
			})
		}
	}

	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].score != ps[j].score {
			return ps[i].score > ps[j].score
		}
		return ps[i].Title < ps[j].Title
	})
	if len(ps) > doctorTop {
		ps = ps[:doctorTop]
	}
	return ps
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	if strings.HasSuffix(unit, "s") {
		return fmt.Sprintf("%d %ses", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
