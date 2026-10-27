package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/ui"
)

func TestPSTable(t *testing.T) {
	useDetectors(t)
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true), aiProc(12, "Cursor", 300<<20, false)})
	out, err := runErr("ps", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TOOL", "ORPHAN", "launchd(1)", "100.0 MB", "yes", "system: 4.0 GB used of 8.0 GB, pressure normal"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Error("table must never show command lines")
	}
}

func TestPSOrphansOnly(t *testing.T) {
	useDetectors(t)
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true), aiProc(12, "Cursor", 300<<20, false)})
	out, err := runErr("ps", "--orphans", "--no-color")
	if err != nil || strings.Contains(out, "Cursor") || !strings.Contains(out, "MCP servers") {
		t.Errorf("err=%v out:\n%s", err, out)
	}
	useProcs(t, []engine.Proc{aiProc(12, "Cursor", 300<<20, false)})
	if _, err := runErr("ps", "--orphans"); exitCode(err) != ExitNothing {
		t.Errorf("no orphans: code=%d; want %d", exitCode(err), ExitNothing)
	}
}

func TestPSNothingFound(t *testing.T) {
	useDetectors(t)
	out, err := runErr("ps")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "No AI-related processes found.") {
		t.Errorf("code=%d out=%q", exitCode(err), out)
	}
}

func TestPSJSONMasksSecrets(t *testing.T) {
	useDetectors(t)
	useProcs(t, []engine.Proc{aiProc(10, "MCP servers", 100<<20, true)})
	out, err := runErr("ps", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("secret leaked into JSON:\n%s", out)
	}
	var doc ui.PSJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not pure JSON: %v", err)
	}
	if len(doc.Processes) != 1 || !doc.Processes[0].Orphan || !doc.Processes[0].OrphanConfident || !strings.Contains(doc.Processes[0].Cmd, "--token ***") {
		t.Errorf("processes = %+v", doc.Processes)
	}
	if doc.System.Pressure != "normal" || len(doc.Groups) != 1 {
		t.Errorf("doc = %+v", doc)
	}
}

func TestPSJSONWatchConflict(t *testing.T) {
	useDetectors(t)
	if _, err := runErr("ps", "--json", "--watch"); exitCode(err) != ExitError {
		t.Errorf("code=%d; want %d", exitCode(err), ExitError)
	}
}

func TestPSWatchRefreshesUntilCancelled(t *testing.T) {
	useDetectors(t)
	frames := 0
	procScan = func(context.Context, engine.Env, *engine.CPUTracker) ([]engine.Proc, engine.MemInfo, error) {
		frames++
		return []engine.Proc{aiProc(10, "MCP servers", 100<<20, true)}, engine.MemInfo{}, nil
	}
	old := watchInterval
	watchInterval = 10 * time.Millisecond
	t.Cleanup(func() { watchInterval = old })

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ps", "--watch", "--no-color"})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("watch must exit cleanly on cancel: %v", err)
	}
	if frames < 3 || strings.Count(out.String(), "TOOL") != frames {
		t.Errorf("frames=%d, headers=%d", frames, strings.Count(out.String(), "TOOL"))
	}
}
