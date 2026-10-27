package engine

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

const (
	// DefaultIdleWindow is how long a process must stay near-zero CPU to count as idle.
	DefaultIdleWindow = 10 * time.Minute
	idleCPUPercent    = 0.5
)

// Proc is one process. Only processes the matcher asked details for have Detail set.
type Proc struct {
	PID, PPID int32
	Name      string
	Cmdline   string // unmasked, in memory only; use MaskCmdline before showing it
	RSS       uint64
	CPUTime   float64 // cumulative CPU seconds
	Created   time.Time
	Detail    bool
	Owned     bool // owned by the current user

	// Set by Analyze.
	Tool       string
	Candidate  bool    // may be reported as an orphan (not an app main process or a service)
	Reparented bool    // chain of wrapper processes ends at init or a dead parent
	Orphan     bool    // candidate && reparented && idle
	Confident  bool    // idleness was observed over the full window, not estimated
	CPU        float64 // percent
	ParentName string
}

// ProcMatch says which tool owns a matched process.
type ProcMatch struct {
	Tool      string
	Candidate bool
}

// ProcMatcher decides whether a process belongs to an AI tool.
type ProcMatcher func(Proc) (ProcMatch, bool)

// Snapshot lists every process with cheap fields (pid, parent, name), and fetches
// cmdline, memory, CPU time and owner only for names detail accepts.
func Snapshot(ctx context.Context, detail func(name string) bool) ([]Proc, error) {
	ps, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	me := os.Getuid()
	meName := ""
	if runtime.GOOS == "windows" {
		meName = currentUsername()
	}
	out := make([]Proc, 0, len(ps))
	for _, p := range ps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, _ := p.NameWithContext(ctx) // keep nameless processes: they still prove a parent is alive
		ppid, _ := p.PpidWithContext(ctx)
		pr := Proc{PID: p.Pid, PPID: ppid, Name: name}
		if detail(name) {
			fillDetail(ctx, p, &pr, me, meName)
		}
		out = append(out, pr)
	}
	return out, nil
}

// CPUTracker watches CPU time across samples so idleness over a window can be judged.
// Keep one per run and call Observe once per snapshot; `ps --watch` reuses it.
type CPUTracker struct {
	Window time.Duration
	seen   map[procKey]*cpuState
}

type procKey struct {
	pid     int32
	created int64
}

type cpuState struct {
	firstSeen, lastActive, at time.Time
	cpu, pct                  float64
	sampled                   bool
}

func NewCPUTracker(window time.Duration) *CPUTracker {
	return &CPUTracker{Window: window, seen: map[procKey]*cpuState{}}
}

// Observe records a sample for every detailed process and forgets the ones that exited.
func (t *CPUTracker) Observe(procs []Proc, now time.Time) {
	live := map[procKey]bool{}
	for _, p := range procs {
		if !p.Detail {
			continue
		}
		k := procKey{p.PID, p.Created.UnixMilli()}
		live[k] = true
		st := t.seen[k]
		if st == nil {
			t.seen[k] = &cpuState{firstSeen: now, lastActive: now, at: now, cpu: p.CPUTime}
			continue
		}
		if dt := now.Sub(st.at).Seconds(); dt > 0 {
			st.pct = (p.CPUTime - st.cpu) / dt * 100
			st.sampled = true
			if st.pct >= idleCPUPercent {
				st.lastActive = now
			}
		}
		st.at, st.cpu = now, p.CPUTime
	}
	for k := range t.seen {
		if !live[k] {
			delete(t.seen, k)
		}
	}
}

// idle reports whether p has been near-zero CPU for the window. confident is false when
// the tracker has not watched long enough and the answer is estimated from lifetime CPU.
func (t *CPUTracker) idle(p Proc, now time.Time) (idle, confident bool) {
	if st := t.seen[procKey{p.PID, p.Created.UnixMilli()}]; st != nil && now.Sub(st.firstSeen) >= t.Window {
		return now.Sub(st.lastActive) >= t.Window, true
	}
	up := now.Sub(p.Created)
	if p.Created.IsZero() || up < t.Window {
		return false, false
	}
	return p.CPUTime/up.Seconds()*100 < idleCPUPercent, false
}

// cpuPercent is the delta since the previous sample, or the lifetime average on the first.
func (t *CPUTracker) cpuPercent(p Proc, now time.Time) float64 {
	if st := t.seen[procKey{p.PID, p.Created.UnixMilli()}]; st != nil && st.sampled {
		return st.pct
	}
	if up := now.Sub(p.Created).Seconds(); !p.Created.IsZero() && up > 0 {
		return p.CPUTime / up * 100
	}
	return 0
}

var wrapperNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true, "env": true,
	"npm": true, "npx": true, "pnpm": true, "yarn": true, "node": true, "bun": true, "deno": true,
	"uv": true, "uvx": true, "cmd": true, "powershell": true, "pwsh": true,
}

func normName(n string) string { return strings.TrimSuffix(strings.ToLower(n), ".exe") }

func isWrapper(n string) bool {
	n = normName(n)
	return wrapperNames[n] || strings.HasPrefix(n, "python")
}

func isInit(p Proc) bool {
	switch normName(p.Name) {
	case "launchd", "systemd", "init":
		return true
	}
	return p.PID <= 1
}

// reparented is true when climbing through wrapper processes (shells, npm, node, python...)
// ends at init or a dead parent: the tool or terminal that started the chain is gone.
// This also covers "original parent tool is not running", since the children of an exited
// tool are reparented to init. A live non-wrapper ancestor (a terminal, tmux, Cursor) means
// the process is still owned by something.
func reparented(p Proc, by map[int32]Proc) bool {
	cur := p
	for range 64 {
		par, ok := by[cur.PPID]
		if cur.PPID <= 1 || !ok || isInit(par) {
			return true
		}
		if !isWrapper(par.Name) {
			return false
		}
		cur = par
	}
	return false
}

// Analyze keeps the user's matched processes and marks orphans. Call tr.Observe(procs, now) first.
func Analyze(procs []Proc, match ProcMatcher, tr *CPUTracker, now time.Time) []Proc {
	by := make(map[int32]Proc, len(procs))
	for _, p := range procs {
		by[p.PID] = p
	}
	var out []Proc
	for _, p := range procs {
		if !p.Detail || !p.Owned {
			continue
		}
		m, ok := match(p)
		if !ok {
			continue
		}
		p.Tool, p.Candidate = m.Tool, m.Candidate
		p.ParentName = by[p.PPID].Name
		p.Reparented = reparented(p, by)
		idle, confident := tr.idle(p, now)
		p.Confident = confident
		p.Orphan = p.Candidate && p.Reparented && idle
		p.CPU = tr.cpuPercent(p, now)
		out = append(out, p)
	}
	return out
}

// ToolGroup sums processes per tool.
type ToolGroup struct {
	Tool      string
	Count     int
	RSS       uint64
	Orphans   int
	OrphanRSS uint64
}

// GroupByTool returns per-tool totals, largest RSS first.
func GroupByTool(procs []Proc) []ToolGroup {
	idx := map[string]*ToolGroup{}
	var groups []*ToolGroup
	for _, p := range procs {
		g := idx[p.Tool]
		if g == nil {
			g = &ToolGroup{Tool: p.Tool}
			idx[p.Tool] = g
			groups = append(groups, g)
		}
		g.Count++
		g.RSS += p.RSS
		if p.Orphan {
			g.Orphans++
			g.OrphanRSS += p.RSS
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].RSS > groups[j].RSS })
	out := make([]ToolGroup, len(groups))
	for i, g := range groups {
		out[i] = *g
	}
	return out
}

// OrphanTotals counts orphaned processes and their memory.
func OrphanTotals(procs []Proc) (count int, rss uint64) {
	for _, p := range procs {
		if p.Orphan {
			count++
			rss += p.RSS
		}
	}
	return
}

var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(--?[\w-]*(?:token|key|secret|password|passwd|auth)[\w-]*)([= ])\S+`), "${1}${2}***"},
	{regexp.MustCompile(`(?i)\b([\w]*(?:token|key|secret|password|passwd)[\w]*)=\S+`), "${1}=***"},
	{regexp.MustCompile(`(?i)bearer\s+\S+`), "Bearer ***"},
	{regexp.MustCompile(`\b(?:sk|ghp|gho|ghs|xox[abp])[-_][A-Za-z0-9_-]{16,}`), "***"},
}

// MaskCmdline hides likely secrets in a command line and truncates it to max runes.
func MaskCmdline(s string, max int) string {
	for _, sp := range secretPatterns {
		s = sp.re.ReplaceAllString(s, sp.repl)
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "…"
	}
	return s
}

// fillDetail reads cmdline, memory, CPU time, start time and owner into pr.
func fillDetail(ctx context.Context, p *process.Process, pr *Proc, me int, meName string) {
	pr.Detail = true
	pr.Cmdline, _ = p.CmdlineWithContext(ctx)
	if mi, err := p.MemoryInfoWithContext(ctx); err == nil {
		pr.RSS = mi.RSS
	}
	if t, err := p.TimesWithContext(ctx); err == nil {
		pr.CPUTime = t.User + t.System
	}
	if ms, err := p.CreateTimeWithContext(ctx); err == nil {
		pr.Created = time.UnixMilli(ms)
	}
	if meName != "" {
		u, _ := p.UsernameWithContext(ctx)
		pr.Owned = u != "" && strings.EqualFold(u, meName)
	} else if uids, err := p.UidsWithContext(ctx); err == nil && len(uids) > 0 {
		pr.Owned = int(uids[0]) == me
	}
}

// IdleNow reports whether the latest CPU sample is near zero. Kill samples twice and
// requires this, so a process that is busy right now is never terminated as an orphan.
func IdleNow(p Proc) bool { return p.CPU < idleCPUPercent }
