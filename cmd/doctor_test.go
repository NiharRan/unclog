package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
)

func useDisk(t *testing.T, d engine.DiskInfo) {
	t.Helper()
	old := readDisk
	readDisk = func(_ context.Context, path string) engine.DiskInfo { d.Path = path; return d }
	t.Cleanup(func() { readDisk = old })
}

var roomy = engine.DiskInfo{Total: 500 << 30, Free: 300 << 30}

func TestDoctorListsProblemsWithFixes(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{
		{Tool: "pnpm", Label: "store", Path: "/home/u/.pnpm", Size: 8 << 30, Category: engine.CategoryCache, Risk: engine.RiskSafe},
		{Tool: "Hugging Face", Label: "m", Path: "/home/u/.cache/hf/m", Size: 3 << 30, Category: engine.CategoryModel, Risk: engine.RiskCareful},
	}})
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 900<<20, true)})
	useDisk(t, roomy)

	out, err := runErr("doctor", "--no-color")
	if err != nil {
		t.Fatalf("err=%v\n%s", err, out)
	}
	for _, want := range []string{"Top 3 problems", "1. pnpm: 8.0 GB reclaimable in 1 item", "fix: unclog clean --category cache",
		"1 orphaned process using 900.0 MB of RAM", "fix: unclog kill --orphans", "fix: unclog clean --category models"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestDoctorHealthyExitsTwo(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{finding("/home/u/.cache/pip", 5<<20)}})
	useDisk(t, roomy)
	out, err := runErr("doctor")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "No problems found.") {
		t.Errorf("code=%d out=%q", exitCode(err), out)
	}
}

func TestDoctorJSON(t *testing.T) {
	useDetectors(t, fake{name: "a", fs: []engine.Finding{
		{Tool: "pnpm", Label: "store", Path: "/home/u/.pnpm", Size: 8 << 30, Category: engine.CategoryCache, Risk: engine.RiskSafe},
	}})
	useDisk(t, engine.DiskInfo{Total: 100 << 30, Free: 1 << 30})
	out, err := runErr("doctor", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc ui.DoctorJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not pure JSON: %v\n%s", err, out)
	}
	if doc.SchemaVersion != ui.SchemaVersion || len(doc.Problems) != 2 || doc.Problems[0].Rank != 1 || doc.Problems[0].Kind != "system" {
		t.Errorf("doc = %+v", doc)
	}
	if doc.Disk.FreeBytes != 1<<30 || doc.Disk.Path == "" || doc.Warnings == nil {
		t.Errorf("disk/warnings = %+v %v", doc.Disk, doc.Warnings)
	}
	healthy := func() string {
		useDetectors(t)
		useDisk(t, roomy)
		o, e := runErr("doctor", "--json")
		if exitCode(e) != ExitNothing {
			t.Errorf("healthy json code = %d", exitCode(e))
		}
		return o
	}()
	if !strings.Contains(healthy, `"problems": []`) {
		t.Errorf("healthy JSON must carry an empty array:\n%s", healthy)
	}
}

func TestDoctorSurvivesDetectorAndProcessErrors(t *testing.T) {
	useDetectors(t, fake{name: "bad", err: context.DeadlineExceeded})
	useDisk(t, engine.DiskInfo{Total: 100 << 30, Free: 4 << 30})
	procScan = func(context.Context, engine.Env, *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
		return nil, engine.MemInfo{}, context.Canceled
	}
	out, err := runErr("doctor", "--json")
	if err != nil || !strings.Contains(out, "bad:") || !strings.Contains(out, "processes:") {
		t.Errorf("err=%v out=%s", err, out)
	}
}
