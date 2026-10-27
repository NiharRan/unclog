package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProc struct {
	p          Proc
	ignoreTerm bool // survives SIGTERM, dies on SIGKILL
	termErr    error
	dead       bool
}

type fakeBackend struct {
	mu    sync.Mutex
	procs map[int32]*fakeProc
	calls []string
}

func (b *fakeBackend) Lookup(_ context.Context, pid int32) (Proc, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if fp, ok := b.procs[pid]; ok && !fp.dead {
		return fp.p, true
	}
	return Proc{}, false
}

func (b *fakeBackend) Term(pid int32) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, fmt.Sprint("TERM ", pid))
	fp := b.procs[pid]
	if fp.termErr != nil {
		return fp.termErr
	}
	if !fp.ignoreTerm {
		fp.dead = true
	}
	return nil
}

func (b *fakeBackend) Kill(pid int32) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, fmt.Sprint("KILL ", pid))
	b.procs[pid].dead = true
	return nil
}

func (b *fakeBackend) Alive(pid int32, _ time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	fp, ok := b.procs[pid]
	return ok && !fp.dead
}

type memLog struct{ lines []string }

func (l *memLog) Log(action, target string, _ Size, outcome string) error {
	l.lines = append(l.lines, action+"|"+target+"|"+outcome)
	return nil
}

var killT0 = time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)

func target1(pid int32, name string) Proc {
	return Proc{PID: pid, PPID: 1, Name: name, Tool: "MCP servers", Owned: true, Detail: true, RSS: 100 << 20, Created: killT0}
}

func mcpOnly(p Proc) (ProcMatch, bool) {
	if strings.HasPrefix(p.Name, "mcp") {
		return ProcMatch{Tool: "MCP servers", Candidate: true}, true
	}
	return ProcMatch{}, false
}

func setup(procs ...*fakeProc) (*Killer, *fakeBackend, *memLog) {
	be := &fakeBackend{procs: map[int32]*fakeProc{}}
	for _, fp := range procs {
		be.procs[fp.p.PID] = fp
	}
	log := &memLog{}
	return &Killer{Backend: be, Match: mcpOnly, Log: log, Self: 4000, Parent: 4001}, be, log
}

func fast() KillOptions { return KillOptions{Grace: 120 * time.Millisecond} }

func TestTerminateGraceful(t *testing.T) {
	k, be, log := setup(&fakeProc{p: target1(500, "mcp-a")})
	res := k.Terminate(context.Background(), []Proc{target1(500, "mcp-a")}, fast())
	if res[0].Outcome != Terminated || res[0].Err != nil {
		t.Fatalf("result = %+v", res[0])
	}
	if !slices.Equal(be.calls, []string{"TERM 500"}) {
		t.Errorf("calls = %v; a polite exit must not be followed by SIGKILL", be.calls)
	}
	if want := []string{"kill|pid 500 mcp-a|started", "kill|pid 500 mcp-a|terminated"}; !slices.Equal(log.lines, want) {
		t.Errorf("log = %v", log.lines)
	}
	if FreedRAM(res) != 100<<20 {
		t.Errorf("freed = %d", FreedRAM(res))
	}
}

func TestTerminateEscalatesToKill(t *testing.T) {
	k, be, _ := setup(&fakeProc{p: target1(500, "mcp-a"), ignoreTerm: true})
	start := time.Now()
	res := k.Terminate(context.Background(), []Proc{target1(500, "mcp-a")}, fast())
	if res[0].Outcome != Killed {
		t.Fatalf("result = %+v", res[0])
	}
	if !slices.Equal(be.calls, []string{"TERM 500", "KILL 500"}) {
		t.Errorf("calls = %v", be.calls)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Error("SIGKILL came before the grace period ended")
	}
}

func TestTerminateRefusals(t *testing.T) {
	other := target1(600, "mcp-other")
	other.Owned = false
	renamed := target1(601, "node-not-mcp")
	k, be, log := setup(
		&fakeProc{p: target1(500, "mcp-a")},
		&fakeProc{p: target1(50, "mcp-low")},
		&fakeProc{p: target1(4000, "mcp-self")},
		&fakeProc{p: target1(4001, "mcp-parent")},
		&fakeProc{p: other},
		&fakeProc{p: renamed},
		&fakeProc{p: func() Proc { p := target1(602, "mcp-reused"); p.Created = killT0.Add(time.Hour); return p }()},
	)
	scanned := target1(602, "mcp-reused") // scan saw an older process with this pid
	targets := []Proc{
		target1(500, "mcp-a"), target1(50, "mcp-low"), target1(4000, "mcp-self"), target1(4001, "mcp-parent"),
		target1(600, "mcp-other"), target1(601, "node-not-mcp"), scanned, {PID: 777},
	}
	res := k.Terminate(context.Background(), targets, fast())

	wantOutcome := []Outcome{Terminated, Refused, Refused, Refused, Refused, Refused, Refused, Refused}
	reasons := []string{"", "below 100", "this program", "parent shell", "another user", "not an AI-related", "different process", "no such process"}
	for i, r := range res {
		if r.Outcome != wantOutcome[i] {
			t.Errorf("%d (pid %d): outcome %s; want %s", i, r.Proc.PID, r.Outcome, wantOutcome[i])
		}
		if reasons[i] != "" && (r.Err == nil || !strings.Contains(r.Err.Error(), reasons[i])) {
			t.Errorf("%d: err = %v; want it to mention %q", i, r.Err, reasons[i])
		}
	}
	if !slices.Equal(be.calls, []string{"TERM 500"}) {
		t.Errorf("signals sent to refused processes: %v", be.calls)
	}
	refusedLogs := 0
	for _, l := range log.lines {
		if strings.Contains(l, "refused") {
			refusedLogs++
		}
	}
	if refusedLogs != 7 {
		t.Errorf("logged %d refusals; want 7 (%v)", refusedLogs, log.lines)
	}
}

func TestTerminateDryRunSendsNothing(t *testing.T) {
	k, be, log := setup(&fakeProc{p: target1(500, "mcp-a")}, &fakeProc{p: target1(50, "mcp-low")})
	res := k.Terminate(context.Background(), []Proc{target1(500, "mcp-a"), target1(50, "mcp-low")}, KillOptions{DryRun: true})
	if res[0].Outcome != DryRun || res[1].Outcome != Refused {
		t.Errorf("results = %+v", res)
	}
	if len(be.calls) != 0 || len(log.lines) != 0 {
		t.Errorf("dry run acted: calls=%v log=%v", be.calls, log.lines)
	}
}

func TestTerminateFillsStubFromFreshLookup(t *testing.T) {
	k, _, _ := setup(&fakeProc{p: target1(500, "mcp-a")})
	res := k.Terminate(context.Background(), []Proc{{PID: 500}}, fast())
	if res[0].Outcome != Terminated || res[0].Proc.Name != "mcp-a" || res[0].Proc.RSS == 0 {
		t.Errorf("result = %+v", res[0])
	}
}

func TestTerminateSignalFailureAndLogFailure(t *testing.T) {
	k, _, _ := setup(&fakeProc{p: target1(500, "mcp-a"), termErr: errors.New("operation not permitted")})
	if res := k.Terminate(context.Background(), []Proc{target1(500, "mcp-a")}, fast()); res[0].Outcome != Failed || !strings.Contains(res[0].Err.Error(), "SIGTERM") {
		t.Errorf("result = %+v", res[0])
	}

	k, be, _ := setup(&fakeProc{p: target1(500, "mcp-a")})
	k.Log = failingLog{}
	res := k.Terminate(context.Background(), []Proc{target1(500, "mcp-a")}, fast())
	if res[0].Outcome != Failed || len(be.calls) != 0 {
		t.Errorf("log failure must stop the kill: %+v calls=%v", res[0], be.calls)
	}
}

type failingLog struct{}

func (failingLog) Log(string, string, Size, string) error { return errors.New("disk full") }

// A real child process: one that exits on SIGTERM and one that ignores it.
func TestTerminateRealProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	spawn := func(script string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", script)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { _ = cmd.Wait() }() // reap, so a dead child is not left as a zombie
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd
	}
	polite := spawn("sleep 60")
	stubborn := spawn(`trap "" TERM; while :; do sleep 1; done`)
	time.Sleep(200 * time.Millisecond) // let the shell install its trap

	be := SystemBackend{}
	match := func(p Proc) (ProcMatch, bool) {
		return ProcMatch{Tool: "test"}, strings.Contains(p.Cmdline, "sleep 60") || strings.Contains(p.Cmdline, `trap "" TERM`)
	}
	lookup := func(c *exec.Cmd) Proc {
		p, ok := be.Lookup(context.Background(), int32(c.Process.Pid))
		if !ok || !p.Owned {
			t.Fatalf("lookup pid %d: ok=%v owned=%v", c.Process.Pid, ok, p.Owned)
		}
		return p
	}
	targets := []Proc{lookup(polite), lookup(stubborn)}

	k := &Killer{Backend: be, Match: match, Log: &memLog{}, Self: int32(os.Getpid()), Parent: int32(os.Getppid())}
	res := k.Terminate(context.Background(), targets, KillOptions{Grace: time.Second})
	if res[0].Outcome != Terminated {
		t.Errorf("polite: %+v", res[0])
	}
	if res[1].Outcome != Killed {
		t.Errorf("stubborn: %+v", res[1])
	}
	for _, c := range []*exec.Cmd{polite, stubborn} {
		if be.Alive(int32(c.Process.Pid), targets[0].Created) {
			t.Errorf("pid %d still alive", c.Process.Pid)
		}
	}
}
