package engine

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// proc builds a detailed, user-owned process that is 2h old with almost no CPU.
func proc(pid, ppid int32, name string) Proc {
	return Proc{PID: pid, PPID: ppid, Name: name, Detail: true, Owned: true, Created: t0.Add(-2 * time.Hour), CPUTime: 1, RSS: 1 << 20}
}

func matchByName(p Proc) (ProcMatch, bool) {
	switch p.Name {
	case "mcp":
		return ProcMatch{Tool: "MCP servers", Candidate: true}, true
	case "Cursor":
		return ProcMatch{Tool: "Cursor"}, true
	case "ollama":
		return ProcMatch{Tool: "Ollama"}, true
	}
	return ProcMatch{}, false
}

func analyze(procs ...Proc) map[int32]Proc {
	tr := NewCPUTracker(DefaultIdleWindow)
	tr.Observe(procs, t0)
	out := map[int32]Proc{}
	for _, p := range Analyze(procs, matchByName, tr, t0) {
		out[p.PID] = p
	}
	return out
}

func TestOrphanRule(t *testing.T) {
	launchd := Proc{PID: 1, Name: "launchd"}
	cases := []struct {
		name   string
		procs  []Proc
		orphan bool
	}{
		{"parent is init", []Proc{launchd, proc(10, 1, "mcp")}, true},
		{"parent is systemd --user", []Proc{launchd, {PID: 900, PPID: 1, Name: "systemd"}, proc(10, 900, "mcp")}, true},
		{"parent gone", []Proc{launchd, proc(10, 555, "mcp")}, true},
		{"wrapper chain ends at init", []Proc{launchd, {PID: 5, PPID: 1, Name: "sh"}, {PID: 6, PPID: 5, Name: "npm"}, proc(10, 6, "mcp")}, true},
		{"live app owns it", []Proc{launchd, {PID: 5, PPID: 1, Name: "Cursor"}, proc(10, 5, "mcp")}, false},
		{"shell under live terminal", []Proc{launchd, {PID: 4, PPID: 1, Name: "Terminal"}, {PID: 5, PPID: 4, Name: "zsh"}, proc(10, 5, "mcp")}, false},
		{"npm wrapper under live app", []Proc{launchd, {PID: 4, PPID: 1, Name: "Cursor"}, {PID: 6, PPID: 4, Name: "node"}, proc(10, 6, "mcp")}, false},
		{"not a candidate: app main", []Proc{launchd, proc(10, 1, "Cursor")}, false},
		{"not a candidate: service", []Proc{launchd, proc(10, 1, "ollama")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := analyze(c.procs...)[10]
			if !ok {
				t.Fatal("process not matched")
			}
			if got.Orphan != c.orphan {
				t.Errorf("Orphan = %v; want %v (reparented=%v)", got.Orphan, c.orphan, got.Reparented)
			}
		})
	}
}

func TestOrphanNeedsIdle(t *testing.T) {
	busy := proc(10, 1, "mcp")
	busy.CPUTime = 2 * 3600 // averaged 100% CPU over its life
	young := proc(11, 1, "mcp")
	young.Created = t0.Add(-time.Minute)
	got := analyze(Proc{PID: 1, Name: "launchd"}, busy, young)
	if got[10].Orphan || got[11].Orphan {
		t.Errorf("busy or young process flagged: busy=%v young=%v", got[10].Orphan, got[11].Orphan)
	}
	if !got[10].Reparented || !got[11].Reparented {
		t.Error("both are reparented; only idleness should block the flag")
	}
}

func TestAnalyzeSkipsOthersAndUnmatched(t *testing.T) {
	other := proc(10, 1, "mcp")
	other.Owned = false
	cheap := Proc{PID: 11, PPID: 1, Name: "mcp"} // no Detail
	unmatched := proc(12, 1, "vim")
	got := analyze(Proc{PID: 1, Name: "launchd"}, other, cheap, unmatched)
	if len(got) != 0 {
		t.Errorf("got %v; want none", got)
	}
}

func TestTrackerConfidence(t *testing.T) {
	p := proc(10, 1, "mcp")
	tr := NewCPUTracker(10 * time.Minute)

	tr.Observe([]Proc{p}, t0)
	if idle, conf := tr.idle(p, t0); !idle || conf {
		t.Errorf("first look: idle=%v conf=%v; want estimated idle", idle, conf)
	}

	// Watched for the full window with no CPU: confident idle.
	end := t0.Add(11 * time.Minute)
	for at := t0.Add(time.Minute); !at.After(end); at = at.Add(time.Minute) {
		tr.Observe([]Proc{p}, at)
	}
	if idle, conf := tr.idle(p, end); !idle || !conf {
		t.Errorf("after window: idle=%v conf=%v; want confident idle", idle, conf)
	}

	// A burst of CPU resets the clock.
	p.CPUTime += 120
	tr.Observe([]Proc{p}, end.Add(time.Minute))
	if idle, _ := tr.idle(p, end.Add(time.Minute)); idle {
		t.Error("process that just burned CPU must not be idle")
	}
	if pct := tr.cpuPercent(p, end.Add(time.Minute)); pct < 100 {
		t.Errorf("cpu%% = %.1f; want a burst above 100", pct)
	}
}

func TestTrackerForgetsExitedProcesses(t *testing.T) {
	tr := NewCPUTracker(time.Minute)
	tr.Observe([]Proc{proc(10, 1, "mcp"), proc(11, 1, "mcp")}, t0)
	tr.Observe([]Proc{proc(10, 1, "mcp")}, t0.Add(time.Second))
	if len(tr.seen) != 1 {
		t.Errorf("tracker holds %d entries; want 1", len(tr.seen))
	}
}

func TestGroupByTool(t *testing.T) {
	a, b, c := proc(1, 1, "x"), proc(2, 1, "x"), proc(3, 1, "x")
	a.Tool, b.Tool, c.Tool = "MCP servers", "MCP servers", "Cursor"
	a.RSS, b.RSS, c.RSS = 100, 50, 500
	b.Orphan = true
	gs := GroupByTool([]Proc{a, b, c})
	if len(gs) != 2 || gs[0].Tool != "Cursor" || gs[1].Count != 2 || gs[1].RSS != 150 || gs[1].Orphans != 1 || gs[1].OrphanRSS != 50 {
		t.Errorf("groups = %+v", gs)
	}
	if n, rss := OrphanTotals([]Proc{a, b, c}); n != 1 || rss != 50 {
		t.Errorf("orphan totals = %d, %d", n, rss)
	}
}

func TestMaskCmdline(t *testing.T) {
	for in, want := range map[string]string{
		"node server.js --token abc123 --port 80":       "node server.js --token *** --port 80",
		"npx srv --api-key=sk-1234567890abcdefgh":       "npx srv --api-key=***",
		"env GITHUB_TOKEN=ghp_abcdefghijklmnop1234 run": "env GITHUB_TOKEN=*** run",
		"curl -H Authorization: Bearer eyJhbGciOi.x.y":  "curl -H Authorization: Bearer ***",
		"node app.js --port 3000":                       "node app.js --port 3000",
	} {
		if got := MaskCmdline(in, 500); got != want {
			t.Errorf("MaskCmdline(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	if got := MaskCmdline(strings.Repeat("a", 50), 10); got != strings.Repeat("a", 10)+"…" {
		t.Errorf("truncate = %q", got)
	}
}

func TestPressure(t *testing.T) {
	for in, want := range map[string]string{"1\n": "normal", "2": "warning", "4": "critical", "9": "", "": ""} {
		if got := darwinPressure(in); got != want {
			t.Errorf("darwinPressure(%q) = %q; want %q", in, got, want)
		}
	}
	psi := "some avg10=%s avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"
	for v, want := range map[string]string{"0.50": "normal", "12.00": "warning", "55.10": "critical"} {
		if got := psiPressure(strings.Replace(psi, "%s", v, 1)); got != want {
			t.Errorf("psi avg10=%s = %q; want %q", v, got, want)
		}
	}
	if psiPressure("garbage") != "" {
		t.Error("garbage PSI should yield no answer")
	}
	for pct, want := range map[float64]string{50: "normal", 85: "warning", 95: "critical"} {
		if got := pressureFromPercent(pct); got != want {
			t.Errorf("pressureFromPercent(%v) = %q; want %q", pct, got, want)
		}
	}
}
