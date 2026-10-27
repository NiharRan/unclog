package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/spf13/cobra"
)

type killRec struct {
	procs  map[int32]engine.Proc
	signal []int32
	logs   []string
}

type recBackend struct{ k *killRec }

func (b recBackend) Lookup(_ context.Context, pid int32) (engine.Proc, bool) {
	p, ok := b.k.procs[pid]
	return p, ok
}
func (b recBackend) Term(pid int32) error {
	b.k.signal = append(b.k.signal, pid)
	delete(b.k.procs, pid)
	return nil
}
func (b recBackend) Kill(int32) error { return nil }
func (b recBackend) Alive(pid int32, _ time.Time) bool {
	_, ok := b.k.procs[pid]
	return ok
}

type recLog struct{ k *killRec }

func (l recLog) Log(a, t string, _ engine.Size, o string) error {
	l.k.logs = append(l.k.logs, a+"|"+t+"|"+o)
	return nil
}

func owned(p engine.Proc) engine.Proc {
	p.Owned, p.Detail, p.Created = true, true, time.Unix(1_700_000_000, 0)
	return p
}

// useKill installs the given matched processes, a recording backend and no sampling delay.
func useKill(t *testing.T, procs ...engine.Proc) *killRec {
	t.Helper()
	rec := &killRec{procs: map[int32]engine.Proc{}}
	for i := range procs {
		procs[i] = owned(procs[i])
		rec.procs[procs[i].PID] = procs[i]
	}
	useDetectors(t)
	useProcs(t, procs)

	oldK, oldG, oldS, oldA, oldI := newKiller, killGrace, sampleGap, askYes, isInteractive
	newKiller = func(engine.Env) *engine.Killer {
		return &engine.Killer{Backend: recBackend{rec}, Log: recLog{rec}, Self: 4000, Parent: 4001,
			Match: func(p engine.Proc) (engine.ProcMatch, bool) { return engine.ProcMatch{Tool: p.Tool}, p.Tool != "" }}
	}
	killGrace, sampleGap = 50*time.Millisecond, 0
	askYes = func(*cobra.Command, string) bool { return true }
	isInteractive = func() bool { return true }
	t.Cleanup(func() { newKiller, killGrace, sampleGap, askYes, isInteractive = oldK, oldG, oldS, oldA, oldI })
	return rec
}

func mcp(pid int32, orphan bool, cpu float64) engine.Proc {
	p := aiProc(pid, "MCP servers", 100<<20, orphan)
	p.CPU = cpu
	return p
}

func TestKillOrphans(t *testing.T) {
	rec := useKill(t, mcp(500, true, 0), mcp(501, false, 0), mcp(502, true, 0))
	out, err := runErr("kill", "--orphans", "--yes", "--no-color")
	if err != nil {
		t.Fatalf("err=%v\n%s", err, out)
	}
	if len(rec.signal) != 2 || rec.signal[0] != 500 || rec.signal[1] != 502 {
		t.Errorf("signalled %v; want the two orphans only", rec.signal)
	}
	if !strings.Contains(out, "Stopped 2 processes, freed about 200.0 MB of RAM.") {
		t.Errorf("output:\n%s", out)
	}
	if len(rec.logs) != 4 {
		t.Errorf("log = %v", rec.logs)
	}
}

func TestKillSkipsOrphansThatAreBusyNow(t *testing.T) {
	rec := useKill(t, mcp(500, true, 35), mcp(502, true, 0))
	if _, err := runErr("kill", "--orphans", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(rec.signal) != 1 || rec.signal[0] != 502 {
		t.Errorf("signalled %v; the busy process must be left alone", rec.signal)
	}
}

func TestKillDryRunSendsNothing(t *testing.T) {
	rec := useKill(t, mcp(500, true, 0))
	out, err := runErr("kill", "--orphans", "--dry-run", "--no-color")
	if err != nil || len(rec.signal) != 0 || len(rec.logs) != 0 {
		t.Fatalf("err=%v signals=%v logs=%v", err, rec.signal, rec.logs)
	}
	if !strings.Contains(out, "would stop") || !strings.Contains(out, "Dry run: 1 process, about 100.0 MB of RAM. Nothing was changed.") {
		t.Errorf("output:\n%s", out)
	}
}

func TestKillPID(t *testing.T) {
	rec := useKill(t, mcp(500, false, 0))
	if _, err := runErr("kill", "--pid", "500", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(rec.signal) != 1 {
		t.Errorf("signals = %v; --pid may target a live matched process", rec.signal)
	}
}

func TestKillUnmatchedAndProtectedPIDsAreRefused(t *testing.T) {
	rec := useKill(t, mcp(500, false, 0))
	out, err := runErr("kill", "--pid", "999", "--pid", "50", "--pid", "4001", "--yes", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.signal) != 0 {
		t.Errorf("signalled %v", rec.signal)
	}
	for _, want := range []string{"no such process", "below 100", "parent shell", "3 processes skipped by safety checks."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestKillConfirmation(t *testing.T) {
	rec := useKill(t, mcp(500, true, 0))
	askYes = func(*cobra.Command, string) bool { return false }
	_, err := runErr("kill", "--orphans")
	if exitCode(err) != ExitCancelled || len(rec.signal) != 0 {
		t.Errorf("declined: code=%d signals=%v", exitCode(err), rec.signal)
	}

	askYes = func(*cobra.Command, string) bool { return true }
	if _, err := runErr("kill", "--orphans"); err != nil || len(rec.signal) != 1 {
		t.Errorf("accepted: err=%v signals=%v", err, rec.signal)
	}
}

func TestKillNeedsYesWithoutTerminal(t *testing.T) {
	rec := useKill(t, mcp(500, true, 0))
	isInteractive = func() bool { return false }
	for _, args := range [][]string{{"kill"}, {"kill", "--orphans"}} {
		if _, err := runErr(args...); exitCode(err) != ExitError {
			t.Errorf("%v: code=%d; want %d", args, exitCode(err), ExitError)
		}
	}
	if len(rec.signal) != 0 {
		t.Error("nothing may be signalled without confirmation")
	}
	if _, err := runErr("kill", "--yes"); err != nil || len(rec.signal) != 1 {
		t.Errorf("--yes alone should stop orphans: err=%v signals=%v", err, rec.signal)
	}
}

func TestKillNothingToStop(t *testing.T) {
	useKill(t, mcp(501, false, 0))
	out, err := runErr("kill", "--orphans", "--yes")
	if exitCode(err) != ExitNothing || !strings.Contains(out, "Nothing to stop.") {
		t.Errorf("code=%d out=%q", exitCode(err), out)
	}
}

func TestKillInteractivePick(t *testing.T) {
	rec := useKill(t, mcp(500, true, 0), mcp(501, false, 0))
	var seen []engine.Item
	pickProcs = func(items []engine.Item) ([]engine.Item, bool, error) {
		seen = append([]engine.Item(nil), items...)
		items[1].Selected = true // the user also ticks the live process
		return items, true, nil
	}
	if _, err := runErr("kill"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !seen[0].Selected || seen[1].Selected {
		t.Errorf("default selection should be the idle orphan only: %+v", seen)
	}
	if len(rec.signal) != 2 {
		t.Errorf("signals = %v", rec.signal)
	}

	rec2 := useKill(t, mcp(500, true, 0))
	pickProcs = func(items []engine.Item) ([]engine.Item, bool, error) { return items, false, nil }
	if _, err := runErr("kill"); exitCode(err) != ExitCancelled || len(rec2.signal) != 0 {
		t.Errorf("cancel: code=%d signals=%v", exitCode(err), rec2.signal)
	}
}

func TestKillRejectsArguments(t *testing.T) {
	useKill(t)
	if _, err := runErr("kill", "1234"); exitCode(err) != ExitError {
		t.Error("kill takes flags, not positional pids")
	}
}
