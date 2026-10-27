package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDetector struct {
	name string
	fs   []Finding
	err  error
	wait bool
}

func (d fakeDetector) Name() string { return d.name }
func (d fakeDetector) Scan(ctx context.Context, _ Env) ([]Finding, error) {
	if d.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return d.fs, d.err
}

func TestScanSortsAndTagsDetector(t *testing.T) {
	ds := []Detector{
		fakeDetector{name: "a", fs: []Finding{{Path: "/small", Size: 1}, {Path: "/b", Size: 10}, {Path: "/a", Size: 10}}},
		fakeDetector{name: "b", fs: []Finding{{Path: "/big", Size: 100, Detector: "custom"}}},
	}
	got, errs := Scan(context.Background(), Env{}, ds, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	order := []string{"/big", "/a", "/b", "/small"}
	for i, p := range order {
		if got[i].Path != p {
			t.Fatalf("order[%d] = %s; want %s (%v)", i, got[i].Path, p, got)
		}
	}
	if got[0].Detector != "custom" || got[1].Detector != "a" {
		t.Errorf("detector tags wrong: %+v", got)
	}
}

func TestScanFailingDetectorKeepsOthers(t *testing.T) {
	boom := errors.New("boom")
	ds := []Detector{
		fakeDetector{name: "bad", err: boom},
		fakeDetector{name: "good", fs: []Finding{{Path: "/x", Size: 1}}},
	}
	got, errs := Scan(context.Background(), Env{}, ds, nil)
	if len(got) != 1 || got[0].Path != "/x" {
		t.Errorf("findings = %v", got)
	}
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Errorf("errs = %v", errs)
	}
}

func TestScanKeepFilter(t *testing.T) {
	ds := []Detector{fakeDetector{name: "a", fs: []Finding{{Path: "/ok", Size: 1}, {Path: "/denied", Size: 2}}}}
	got, _ := Scan(context.Background(), Env{}, ds, func(f Finding) bool { return f.Path != "/denied" })
	if len(got) != 1 || got[0].Path != "/ok" {
		t.Errorf("findings = %v", got)
	}
}

func TestScanCancelledReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan []error)
	go func() {
		_, errs := Scan(ctx, Env{}, []Detector{fakeDetector{name: "slow", wait: true}}, nil)
		done <- errs
	}()
	select {
	case errs := <-done:
		if len(errs) != 1 || !errors.Is(errs[0], context.DeadlineExceeded) {
			t.Errorf("errs = %v", errs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Scan did not return after context cancel")
	}
}
