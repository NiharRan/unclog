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

// TrashedEntry is a past clean that can still be undone.
type TrashedEntry struct {
	Time       time.Time
	Orig, Dest string // Dest is where it sits in the Trash, "" if the log did not record it
	Size       Size
	Undo       func() error // moves it back to Orig; set by the Locator
}

// Locator finds e in the trash and returns how to undo it, or false if it is gone.
type Locator func(e TrashedEntry) (undo func() error, ok bool)

// InTrash locates entries by the trash path recorded in the log, for a trash that is a
// plain folder such as ~/.Trash on macOS. Entries without a recorded path are not found.
func InTrash(trashDir string) Locator {
	return func(e TrashedEntry) (func() error, bool) {
		if e.Dest == "" || filepath.Dir(e.Dest) != trashDir {
			return nil, false
		}
		if _, err := os.Lstat(e.Dest); err != nil {
			return nil, false
		}
		return func() error { return renameNoReplace(e.Dest, e.Orig) }, true
	}
}

// ListTrashed reads the history log and returns trash actions that locate still finds in
// the trash, newest first.
func ListTrashed(logPath string, locate Locator) ([]TrashedEntry, error) {
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
		if !ok {
			dest = "" // CutPrefix hands back the whole string on a miss
			if outcome != string(Trashed) {
				continue
			}
		}
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		e := TrashedEntry{Time: ts, Orig: orig, Dest: dest, Size: Size(size)}
		if undo, found := locate(e); found {
			e.Undo = undo
			out = append(out, e)
		}
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
	if why := r.Denied(resolveParents(e.Orig)); why != "" {
		return fmt.Errorf("refused to restore %s: its folder now resolves to a protected place (%s)", e.Orig, why)
	}
	if _, err := os.Lstat(e.Orig); err == nil {
		return fmt.Errorf("%s already exists, not overwriting", e.Orig)
	}
	if err := r.Log.Log("restore", e.Orig, e.Size, "started"); err != nil {
		return fmt.Errorf("not acting, history log unwritable: %w", err)
	}
	err := os.MkdirAll(filepath.Dir(e.Orig), 0o755)
	if err == nil {
		err = e.Undo()
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

// resolveParents returns p with symlinks resolved in its deepest existing ancestor, so a
// folder that has since become a symlink cannot steer a restore past the denylist.
func resolveParents(p string) string {
	dir, rest := filepath.Dir(p), filepath.Base(p)
	for dir != filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = filepath.Dir(dir)
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return filepath.Join(dir, rest)
}
