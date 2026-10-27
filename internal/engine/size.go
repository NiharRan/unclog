package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const walkWorkers = 16

// DirSize returns the total apparent size of regular files under path.
// Symlinks are never followed or counted; unreadable entries are skipped.
// shortcut: apparent size, not allocated blocks, and hardlinks count twice; upgrade if numbers drift from du.
func DirSize(ctx context.Context, path string) (Size, error) {
	s, _, err := Measure(ctx, path)
	return s, err
}

// LastUsed returns the newest modification time of any regular file under path.
func LastUsed(ctx context.Context, path string) (time.Time, error) {
	_, t, err := Measure(ctx, path)
	return t, err
}

type walker struct {
	size   atomic.Int64
	newest atomic.Int64 // unix nanos
	sem    chan struct{}
	wg     sync.WaitGroup
}

// Measure returns DirSize and LastUsed from a single walk.
func Measure(ctx context.Context, root string) (Size, time.Time, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		return 0, time.Time{}, err
	}
	w := &walker{sem: make(chan struct{}, walkWorkers)}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
	case fi.IsDir():
		w.dir(ctx, root)
		w.wg.Wait()
	default:
		w.file(fi)
	}
	if err := ctx.Err(); err != nil {
		return 0, time.Time{}, err
	}
	var t time.Time
	if n := w.newest.Load(); n != 0 {
		t = time.Unix(0, n)
	}
	return Size(w.size.Load()), t, nil
}

func (w *walker) file(fi os.FileInfo) {
	w.size.Add(fi.Size())
	n := fi.ModTime().UnixNano()
	for {
		cur := w.newest.Load()
		if n <= cur || w.newest.CompareAndSwap(cur, n) {
			return
		}
	}
}

func (w *walker) dir(ctx context.Context, path string) {
	if ctx.Err() != nil {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		fi, err := e.Info() // lstat-based: does not follow symlinks
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(path, e.Name())
		if !fi.IsDir() {
			w.file(fi)
			continue
		}
		// Recurse in a new goroutine only if a worker slot is free; otherwise
		// inline, so a full pool can never deadlock.
		select {
		case w.sem <- struct{}{}:
			w.wg.Add(1)
			go func() {
				defer func() { <-w.sem; w.wg.Done() }()
				w.dir(ctx, child)
			}()
		default:
			w.dir(ctx, child)
		}
	}
}
