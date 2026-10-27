package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

const (
	Terminated Outcome = "terminated"
	Killed     Outcome = "killed" // needed SIGKILL after the grace period
)

// DefaultGrace is how long a process gets after SIGTERM before SIGKILL.
const DefaultGrace = 5 * time.Second

// ProcBackend is the operating system side of killing; tests inject a fake.
type ProcBackend interface {
	// Lookup re-reads a live process; ok is false if it does not exist.
	Lookup(ctx context.Context, pid int32) (p Proc, ok bool)
	Term(pid int32) error
	Kill(pid int32) error
	// Alive is false once the process (identified by start time) has exited.
	Alive(pid int32, created time.Time) bool
}

type KillOptions struct {
	DryRun bool
	Grace  time.Duration // 0 means DefaultGrace
}

type KillResult struct {
	Proc    Proc
	Outcome Outcome
	Err     error
}

// Killer stops matched AI processes politely: SIGTERM, wait, then SIGKILL.
// It only ever signals the targets it is given, never their children or parents.
type Killer struct {
	Backend ProcBackend
	Match   ProcMatcher
	Log     ActionLog
	Self    int32 // pids that are never touched: this process and its parent shell
	Parent  int32
}

func NewKiller(match ProcMatcher, log ActionLog) *Killer {
	return &Killer{Backend: SystemBackend{}, Match: match, Log: log, Self: int32(os.Getpid()), Parent: int32(os.Getppid())}
}

// Terminate refuses anything unsafe and stops the rest. Per-process problems never stop others.
// Rules, each logged to history: not this process or its parent, PID >= 100, still the same
// process (start time), owned by the current user, and still matched by a detector.
func (k *Killer) Terminate(ctx context.Context, targets []Proc, o KillOptions) []KillResult {
	grace := o.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	results := make([]KillResult, len(targets))
	var live []int // indexes of targets that were signalled
	for i, t := range targets {
		results[i] = KillResult{Proc: t}
		if err := k.check(ctx, &results[i]); err != nil {
			results[i].Outcome, results[i].Err = Refused, err
			if !o.DryRun {
				_ = k.Log.Log("kill", target(t), Size(t.RSS), "refused: "+err.Error())
			}
			continue
		}
		if o.DryRun {
			results[i].Outcome = DryRun
			continue
		}
		if err := k.Log.Log("kill", target(t), Size(t.RSS), "started"); err != nil {
			results[i].Outcome, results[i].Err = Failed, fmt.Errorf("not acting, history log unwritable: %w", err)
			continue
		}
		if err := k.Backend.Term(t.PID); err != nil && k.Backend.Alive(t.PID, t.Created) {
			k.finish(&results[i], Failed, fmt.Errorf("SIGTERM: %w", err))
			continue
		}
		live = append(live, i)
	}

	dead, alive := k.waitDead(ctx, targets, live, grace)
	for _, i := range dead {
		k.finish(&results[i], Terminated, nil)
	}
	var forced []int
	for _, i := range alive { // still running after the grace period
		t := targets[i]
		if err := k.Backend.Kill(t.PID); err != nil && k.Backend.Alive(t.PID, t.Created) {
			k.finish(&results[i], Failed, fmt.Errorf("SIGKILL: %w", err))
			continue
		}
		forced = append(forced, i)
	}
	dead, alive = k.waitDead(ctx, targets, forced, 2*time.Second)
	for _, i := range dead {
		k.finish(&results[i], Killed, nil)
	}
	for _, i := range alive {
		k.finish(&results[i], Failed, fmt.Errorf("still running after SIGKILL"))
	}
	return results
}

// waitDead polls until every target in idx has exited or d passes.
func (k *Killer) waitDead(ctx context.Context, targets []Proc, idx []int, d time.Duration) (dead, alive []int) {
	deadline := time.Now().Add(d)
	for {
		alive = alive[:0]
		for _, i := range idx {
			if k.Backend.Alive(targets[i].PID, targets[i].Created) {
				alive = append(alive, i)
			} else {
				dead = append(dead, i)
			}
		}
		idx = append([]int(nil), alive...)
		if len(idx) == 0 || time.Now().After(deadline) || ctx.Err() != nil {
			return dead, idx
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (k *Killer) finish(r *KillResult, outcome Outcome, err error) {
	r.Outcome, r.Err = outcome, err
	msg := string(outcome)
	if err != nil {
		msg += ": " + err.Error()
	}
	_ = k.Log.Log("kill", target(r.Proc), Size(r.Proc.RSS), msg)
}

func target(p Proc) string { return fmt.Sprintf("pid %d %s", p.PID, p.Name) }

// check applies the refusal rules; it fills in the fresh process data on success.
func (k *Killer) check(ctx context.Context, r *KillResult) error {
	t := r.Proc
	switch {
	case t.PID == k.Self || t.PID == k.Parent:
		return fmt.Errorf("pid %d is this program or its parent shell", t.PID)
	case t.PID < 100:
		return fmt.Errorf("pid %d is below 100", t.PID)
	}
	fresh, ok := k.Backend.Lookup(ctx, t.PID)
	switch {
	case !ok:
		return fmt.Errorf("no such process")
	case !t.Created.IsZero() && !fresh.Created.Equal(t.Created):
		return fmt.Errorf("pid %d is a different process now (start time changed)", t.PID)
	case !fresh.Owned:
		return fmt.Errorf("owned by another user")
	}
	if _, matched := k.Match(fresh); !matched {
		return fmt.Errorf("not an AI-related process")
	}
	if r.Proc.Name == "" {
		r.Proc.Name, r.Proc.RSS, r.Proc.Created, r.Proc.Tool = fresh.Name, fresh.RSS, fresh.Created, fresh.Tool
	}
	return nil
}

// FreedRAM sums the memory of processes that actually exited.
func FreedRAM(results []KillResult) uint64 {
	var n uint64
	for _, r := range results {
		if r.Outcome == Terminated || r.Outcome == Killed {
			n += r.Proc.RSS
		}
	}
	return n
}

// SystemBackend signals real processes through gopsutil.
type SystemBackend struct{}

func (SystemBackend) Lookup(ctx context.Context, pid int32) (Proc, bool) {
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return Proc{}, false
	}
	name, _ := p.NameWithContext(ctx)
	ppid, _ := p.PpidWithContext(ctx)
	pr := Proc{PID: pid, PPID: ppid, Name: name}
	meName := ""
	if runtime.GOOS == "windows" {
		meName = currentUsername()
	}
	fillDetail(ctx, p, &pr, os.Getuid(), meName)
	return pr, true
}

// Term sends SIGTERM. shortcut: on Windows gopsutil can only terminate hard, so there is no
// graceful close request there; upgrade by sending a console close event first.
func (SystemBackend) Term(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.SendSignal(syscall.SIGTERM)
}

func (SystemBackend) Kill(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func (SystemBackend) Alive(pid int32, created time.Time) bool {
	p, err := process.NewProcess(pid)
	if err != nil {
		return false
	}
	if ms, err := p.CreateTime(); err != nil || (!created.IsZero() && ms != created.UnixMilli()) {
		return false
	}
	if st, err := p.Status(); err == nil && len(st) > 0 && (st[0] == "zombie" || st[0] == "Z") {
		return false
	}
	return true
}

// ProcLabel is the short form used in lists: "node (pid 123)".
func ProcLabel(p Proc) string {
	if p.Name == "" {
		return fmt.Sprintf("pid %d", p.PID)
	}
	return fmt.Sprintf("%s (pid %d)", p.Name, p.PID)
}
