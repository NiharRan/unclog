package engine

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TrashedEntry is a past clean that can still be undone: Dest is where it sits in the Trash.
type TrashedEntry struct {
	Time       time.Time
	Orig, Dest string
	Size       Size
}

// ListTrashed reads the history log and returns trash actions whose file is still in
// trashDir, newest first. Lines without a recorded trash location are skipped.
func ListTrashed(logPath, trashDir string) ([]TrashedEntry, error) {
	f, err := os.Open(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []TrashedEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) != 5 || p[1] != "trash" {
			continue
		}
		orig, e1 := strconv.Unquote(p[2])
		outcome, e2 := strconv.Unquote(p[4])
		ts, e3 := time.Parse(time.RFC3339, p[0])
		size, e4 := strconv.ParseInt(p[3], 10, 64)
		dest, ok := strings.CutPrefix(outcome, string(Trashed)+TrashedTo)
		if !ok || e1 != nil || e2 != nil || e3 != nil || e4 != nil || filepath.Dir(dest) != trashDir {
			continue
		}
		if _, err := os.Lstat(dest); err != nil {
			continue
		}
		out = append(out, TrashedEntry{Time: ts, Orig: orig, Dest: dest, Size: Size(size)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, sc.Err()
}

// Restorer moves trashed files back to where they came from.
type Restorer struct {
	Log ActionLog
	// Denied returns why a path must never be touched, or "" if it may be.
	Denied func(path string) string
}

// Restore puts e back at its original path. It never overwrites anything.
func (r *Restorer) Restore(e TrashedEntry) error {
	if why := r.Denied(e.Orig); why != "" {
		return fmt.Errorf("refused to restore %s: %s", e.Orig, why)
	}
	if _, err := os.Lstat(e.Dest); err != nil {
		return fmt.Errorf("%s is no longer in the Trash", e.Dest)
	}
	if _, err := os.Lstat(e.Orig); err == nil {
		return fmt.Errorf("%s already exists, not overwriting", e.Orig)
	}
	if err := r.Log.Log("restore", e.Orig, e.Size, "started"); err != nil {
		return fmt.Errorf("not acting, history log unwritable: %w", err)
	}
	err := os.MkdirAll(filepath.Dir(e.Orig), 0o755)
	if err == nil {
		err = os.Rename(e.Dest, e.Orig)
	}
	outcome := "restored"
	if err != nil {
		outcome = "failed: " + err.Error()
	}
	if lerr := r.Log.Log("restore", e.Orig, e.Size, outcome); lerr != nil && err == nil {
		err = fmt.Errorf("restored, but history log unwritable: %w", lerr)
	}
	return err
}
