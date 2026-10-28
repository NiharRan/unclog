package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Scan runs all detectors concurrently and returns findings, largest first.
// One failing detector never aborts the rest; its error is returned alongside.
// keep, if non-nil, drops findings it rejects (callers pass a safety denylist check).
// Detectors must honour ctx: Scan waits for all of them.
func Scan(ctx context.Context, env Env, ds []Detector, keep func(Finding) bool) ([]Finding, []error) {
	return ScanProgress(ctx, env, ds, keep, nil)
}

// ScanProgress is Scan that calls onDone(done, total, name) each time a detector finishes.
// onDone may be called from several goroutines, but never concurrently.
func ScanProgress(ctx context.Context, env Env, ds []Detector, keep func(Finding) bool, onDone func(done, total int, name string)) ([]Finding, []error) {
	var mu sync.Mutex
	done := 0
	res := make([][]Finding, len(ds))
	errs := make([]error, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if onDone != nil {
					mu.Lock()
					done++
					onDone(done, len(ds), d.Name())
					mu.Unlock()
				}
			}()
			fs, err := d.Scan(ctx, env)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", d.Name(), err)
			}
			for _, f := range fs {
				if f.Detector == "" {
					f.Detector = d.Name()
				}
				if keep == nil || keep(f) {
					res[i] = append(res[i], f)
				}
			}
		}()
	}
	wg.Wait()

	var all []Finding
	var failed []error
	for i := range ds {
		all = append(all, res[i]...)
		if errs[i] != nil {
			failed = append(failed, errs[i])
		}
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].Size != all[b].Size {
			return all[a].Size > all[b].Size
		}
		return all[a].Path < all[b].Path
	})
	return all, failed
}

// Total sums finding sizes.
func Total(fs []Finding) Size {
	var t Size
	for _, f := range fs {
		t += f.Size
	}
	return t
}

// FilterMinSize drops findings smaller than min.
func FilterMinSize(fs []Finding, min Size) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Size >= min {
			out = append(out, f)
		}
	}
	return out
}
