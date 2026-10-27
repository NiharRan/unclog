package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

// Entry is one logged clean or kill action.
type Entry struct {
	Time    time.Time
	Action  string // trash, delete, kill
	Target  string // path or PID
	Size    engine.Size
	Outcome string
}

// History appends actions to ~/.config/aiclean/history.log.
type History struct {
	path string
	mu   sync.Mutex
}

func NewHistory(env engine.Env) *History {
	return &History{path: filepath.Join(paths.ConfigDir(env), "aiclean", "history.log")}
}

func (h *History) Path() string { return h.path }

// Append writes one tab-separated line: time, action, quoted target, bytes, quoted outcome.
// Callers must treat an error as a failed action: nothing may be removed unlogged.
func (h *History) Append(e Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%d\t%s\n", e.Time.UTC().Format(time.RFC3339), e.Action,
		strconv.Quote(e.Target), int64(e.Size), strconv.Quote(e.Outcome))
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
