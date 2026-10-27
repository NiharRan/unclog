package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Item is a finding in a clean plan.
type Item struct {
	Finding
	Selected bool
}

// PlanOptions filter which findings the plan lists. Nothing here selects risky items.
type PlanOptions struct {
	Categories map[Category]bool // empty means all
	OlderThan  time.Duration     // 0 means off; findings with an unknown last-used date are dropped when set
	Now        time.Time
}

// BuildPlan filters and orders findings (tool groups largest first) and applies the
// default selection: safe items whose tool is not running. Review and careful items are
// listed but unchecked. Dry runs and real runs both start from this one function, so
// they always list the same items.
func BuildPlan(fs []Finding, o PlanOptions) []Item {
	var keep []Finding
	for _, f := range fs {
		if len(o.Categories) > 0 && !o.Categories[f.Category] {
			continue
		}
		if o.OlderThan > 0 && (f.LastUsed.IsZero() || o.Now.Sub(f.LastUsed) < o.OlderThan) {
			continue
		}
		keep = append(keep, f)
	}
	items := make([]Item, 0, len(keep))
	for _, f := range SortGrouped(keep) {
		items = append(items, Item{Finding: f, Selected: f.Risk == RiskSafe && !f.ToolRunning})
	}
	return items
}

// SortGrouped orders findings by tool group (largest first), then by size.
func SortGrouped(fs []Finding) []Finding {
	totals := map[string]Size{}
	for _, f := range fs {
		totals[f.Tool] += f.Size
	}
	out := append([]Finding(nil), fs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Tool != b.Tool && totals[a.Tool] != totals[b.Tool]:
			return totals[a.Tool] > totals[b.Tool]
		case a.Tool != b.Tool:
			return a.Tool < b.Tool
		}
		return a.Size > b.Size
	})
	return out
}

// Selected returns the findings marked for removal.
func Selected(items []Item) []Item {
	var out []Item
	for _, it := range items {
		if it.Selected {
			out = append(out, it)
		}
	}
	return out
}

// ParseAge parses "30d", "2w", "12h" or any time.ParseDuration string.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	for suffix, unit := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(v * float64(unit)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid age %q", s)
	}
	return d, nil
}

// ParseCategories accepts "model", "models", "cache", "tmp", "logs" (case-insensitive).
func ParseCategories(in []string) (map[Category]bool, error) {
	out := map[Category]bool{}
	for _, raw := range in {
		for _, s := range strings.Split(raw, ",") {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" {
				continue
			}
			switch s {
			case "model", "models":
				out[CategoryModel] = true
			case "cache", "caches":
				out[CategoryCache] = true
			case "tmp", "temp":
				out[CategoryTmp] = true
			case "log", "logs":
				out[CategoryLogs] = true
			case "process", "processes":
				out[CategoryProcess] = true
			default:
				return nil, fmt.Errorf("unknown category %q (use model, cache, tmp or logs)", s)
			}
		}
	}
	return out, nil
}

// Guard decides whether a finding may be deleted; safety.Checker implements it.
type Guard interface {
	Validate(f Finding, detectorRoot string) error
}

// ActionLog records every clean action; safety.History implements it.
type ActionLog interface {
	Log(action, target string, size Size, outcome string) error
}

type Outcome string

const (
	Trashed Outcome = "trashed"
	Deleted Outcome = "deleted"
	Ran     Outcome = "ran"
	Refused Outcome = "refused"
	Failed  Outcome = "failed"
	DryRun  Outcome = "dry-run"
)

// Result is what happened to one selected item.
type Result struct {
	Finding Finding
	Action  string // trash, delete or run
	Outcome Outcome
	Err     error // reason for Refused and Failed
}

type ExecOptions struct {
	Permanent bool
	DryRun    bool
}

// Cleaner removes selected findings. Every dependency is injected so tests never touch
// the real trash, log or docker.
type Cleaner struct {
	Guard Guard
	Log   ActionLog
	Trash func(path string) error
	// Exec runs a CleanVia command; it has no short timeout because prunes can be slow.
	Exec func(ctx context.Context, argv []string) error
}

// viaAllowed lists the programs a detector may ask us to run to remove something.
var viaAllowed = map[string]bool{"docker": true, "ollama": true}

// DefaultExec runs argv and returns its combined output in the error on failure.
func DefaultExec(ctx context.Context, argv []string) error {
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Execute processes the selected items in order. Per-item problems never stop the rest.
// For a real run each action is logged before it starts and again with its outcome, so a
// failing log stops the action instead of leaving an unlogged removal.
func (c *Cleaner) Execute(ctx context.Context, items []Item, o ExecOptions) []Result {
	var results []Result
	for _, it := range Selected(items) {
		if ctx.Err() != nil {
			break
		}
		results = append(results, c.one(ctx, it.Finding, o))
	}
	return results
}

func (c *Cleaner) one(ctx context.Context, f Finding, o ExecOptions) Result {
	action, target := "trash", f.Path
	switch {
	case len(f.CleanVia) > 0:
		action, target = "run", strings.Join(f.CleanVia, " ")
	case o.Permanent:
		action = "delete"
	}
	r := Result{Finding: f, Action: action}

	refuse := func(err error) Result {
		r.Outcome, r.Err = Refused, err
		if !o.DryRun {
			_ = c.Log.Log(action, target, f.Size, "refused: "+err.Error())
		}
		return r
	}

	if action == "run" {
		if !viaAllowed[f.CleanVia[0]] {
			return refuse(fmt.Errorf("refused to run %q: not an allowed cleanup command", f.CleanVia[0]))
		}
	} else {
		if err := c.Guard.Validate(f, filepath.Dir(f.Path)); err != nil {
			return refuse(err)
		}
		if _, err := os.Lstat(f.Path); err != nil {
			return refuse(fmt.Errorf("%s no longer exists", f.Path))
		}
	}
	if o.DryRun {
		r.Outcome = DryRun
		return r
	}

	if err := c.Log.Log(action, target, f.Size, "started"); err != nil {
		r.Outcome, r.Err = Failed, fmt.Errorf("not acting, history log unwritable: %w", err)
		return r
	}
	var err error
	switch action {
	case "run":
		err = c.Exec(ctx, f.CleanVia)
		r.Outcome = Ran
	case "delete":
		err = os.RemoveAll(f.Path)
		r.Outcome = Deleted
	default:
		err = c.Trash(f.Path)
		r.Outcome = Trashed
	}
	outcome := string(r.Outcome)
	if err != nil {
		r.Outcome, r.Err = Failed, err
		outcome = "failed: " + err.Error()
	}
	if lerr := c.Log.Log(action, target, f.Size, outcome); lerr != nil && r.Err == nil {
		r.Err = fmt.Errorf("done, but history log unwritable: %w", lerr)
	}
	return r
}

// Freed sums the size of items that were actually removed.
func Freed(results []Result) Size {
	var s Size
	for _, r := range results {
		switch r.Outcome {
		case Trashed, Deleted, Ran:
			s += r.Finding.Size
		}
	}
	return s
}

// ErrCancelled is returned when the user backs out.
var ErrCancelled = errors.New("cancelled")
