package engine

import (
	"strings"
	"testing"
)

func finding(tool string, size Size, cat Category) Finding {
	return Finding{Tool: tool, Label: tool, Path: "/h/" + tool, Size: size, Category: cat, Risk: RiskSafe}
}

var okMem = MemInfo{Total: 16 << 30, Used: 4 << 30, Available: 12 << 30, Pressure: "normal"}
var okDisk = DiskInfo{Path: "/h", Total: 500 << 30, Free: 300 << 30}

func TestDiagnoseHealthy(t *testing.T) {
	small := []Finding{finding("pip", 5<<20, CategoryCache)}
	if got := Diagnose(small, nil, okMem, okDisk); len(got) != 0 {
		t.Errorf("healthy machine got %+v", got)
	}
	if got := Diagnose(nil, nil, MemInfo{}, DiskInfo{}); len(got) != 0 {
		t.Errorf("unknown system got %+v", got)
	}
}

func TestDiagnoseRanksAndTopFive(t *testing.T) {
	var fs []Finding
	for i, tool := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		fs = append(fs, finding(tool, Size(200+i*100)<<20, CategoryCache))
	}
	got := Diagnose(fs, nil, okMem, okDisk)
	if len(got) != 5 {
		t.Fatalf("got %d problems; want the top 5", len(got))
	}
	if !strings.HasPrefix(got[0].Title, "g:") || !strings.HasPrefix(got[4].Title, "c:") {
		t.Errorf("order: %q ... %q", got[0].Title, got[4].Title)
	}
}

func TestDiagnoseFixMatchesDominantCategory(t *testing.T) {
	fs := []Finding{
		finding("Hugging Face", 3<<30, CategoryModel),
		finding("Hugging Face", 50<<20, CategoryCache),
		finding("Claude Code", 2<<30, CategoryLogs),
		finding("pip", 400<<20, CategoryCache),
	}
	fixes := map[string]string{}
	for _, p := range Diagnose(fs, nil, okMem, okDisk) {
		fixes[strings.SplitN(p.Title, ":", 2)[0]] = p.Fix
	}
	want := map[string]string{
		"Hugging Face": "unclog clean --category models",
		"Claude Code":  "unclog clean --category logs",
		"pip":          "unclog clean --category cache",
	}
	for tool, fix := range want {
		if fixes[tool] != fix {
			t.Errorf("%s fix = %q; want %q", tool, fixes[tool], fix)
		}
	}
}

func TestDiagnoseOrphans(t *testing.T) {
	procs := []Proc{{PID: 1, Orphan: true, RSS: 600 << 20}, {PID: 2, Orphan: true, RSS: 400 << 20}, {PID: 3, RSS: 9 << 30}}
	got := Diagnose([]Finding{finding("big", 3<<30, CategoryCache)}, procs, okMem, okDisk)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	// 1 GB of orphaned RAM is weighted above 3 GB of cache; a live 9 GB process is not a problem.
	if got[0].Kind != "ram" || got[0].Fix != "unclog kill --orphans" || !strings.Contains(got[0].Title, "2 orphaned processes using 1000.0 MB") {
		t.Errorf("top = %+v", got[0])
	}
}

func TestDiagnoseSystemPressure(t *testing.T) {
	fs := []Finding{finding("big", 20<<30, CategoryCache)}
	warn := MemInfo{Total: 8 << 30, Used: 7 << 30, Pressure: "warning"}
	got := Diagnose(fs, nil, warn, okDisk)
	if len(got) != 2 || got[0].Kind != "disk" || got[1].Kind != "system" || got[1].Fix != "unclog ps" {
		t.Errorf("warning: %+v", got)
	}
	crit := MemInfo{Total: 8 << 30, Used: 8 << 30, Pressure: "critical"}
	if got := Diagnose(fs, nil, crit, okDisk); got[0].Kind != "system" || !strings.Contains(got[0].Title, "critical") {
		t.Errorf("critical must outrank everything: %+v", got)
	}
}

func TestDiagnoseLowDisk(t *testing.T) {
	low := DiskInfo{Path: "/h", Total: 500 << 30, Free: 8 << 30}
	got := Diagnose(nil, nil, okMem, low)
	if len(got) != 1 || got[0].Kind != "system" || got[0].Fix != "unclog clean" || !strings.Contains(got[0].Title, "Only 8.0 GB free") {
		t.Errorf("low disk: %+v", got)
	}
	roomy := DiskInfo{Path: "/h", Total: 1000 << 30, Free: 150 << 30}
	if len(Diagnose(nil, nil, okMem, roomy)) != 0 {
		t.Error("150 GB free (15%) should not alarm")
	}
	pct := DiskInfo{Path: "/h", Total: 1000 << 30, Free: 50 << 30}
	if len(Diagnose(nil, nil, okMem, pct)) != 1 {
		t.Error("50 GB free is only 5% of the disk and should alarm")
	}
	tiny := DiskInfo{Path: "/h", Total: 100 << 30, Free: 5 << 30}
	if got := Diagnose(nil, nil, okMem, tiny); len(got) != 1 {
		t.Errorf("5%% of a small disk: %+v", got)
	}
}
