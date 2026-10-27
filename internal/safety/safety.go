package safety

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/NiharRan/unclog/internal/engine"
)

// ErrRefused is wrapped by every refusal so callers can errors.Is it.
var ErrRefused = errors.New("refused")

func refuse(p, reason string) error { return fmt.Errorf("%w %s: %s", ErrRefused, p, reason) }

// Checker runs every deletion check. Open and Procs are injectable for tests.
type Checker struct {
	Env   engine.Env
	Open  func() []string // paths open by running processes
	Procs func() []string // running process names

	once sync.Once
	open []string
}

func NewChecker(env engine.Env) *Checker {
	return &Checker{
		Env:  env,
		Open: func() []string { return listOpenFiles(env) },
		Procs: func() []string {
			if env.ProcessNames == nil {
				return nil
			}
			return env.ProcessNames()
		},
	}
}

// ResolveWithin resolves symlinks in path and root and errors if the real path escapes root.
func ResolveWithin(p, root string) (string, error) {
	rp, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rr, rp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s resolves to %s, outside %s", p, rp, rr)
	}
	return rp, nil
}

// Validate returns nil only if the finding's path may be deleted: not denied, not a
// config or auth file, inside detectorRoot after symlink resolution, and not open.
// A running tool is not a hard refusal; callers ask the user (see ToolRunning).
func (c *Checker) Validate(f engine.Finding, detectorRoot string) error {
	if denied, why := IsDenied(f.Path, c.Env); denied {
		return refuse(f.Path, why)
	}
	if prot, why := IsProtected(f.Path, c.Env); prot {
		return refuse(f.Path, why)
	}
	real, err := ResolveWithin(f.Path, detectorRoot)
	if err != nil {
		return refuse(f.Path, err.Error())
	}
	if denied, why := IsDenied(real, c.Env); denied {
		return refuse(f.Path, "resolves to "+real+": "+why)
	}
	if prot, why := IsProtected(real, c.Env); prot {
		return refuse(f.Path, "resolves to "+real+": "+why)
	}
	if c.IsOpen(real) {
		return refuse(f.Path, "in use by a running process")
	}
	return nil
}
