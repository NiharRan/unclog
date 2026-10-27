// Package engine is the shared core used by the CLI and the future desktop app.
package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Category string

const (
	CategoryModel   Category = "model"
	CategoryCache   Category = "cache"
	CategoryTmp     Category = "tmp"
	CategoryLogs    Category = "logs"
	CategoryProcess Category = "process"
)

type Risk string

const (
	RiskSafe    Risk = "safe"
	RiskReview  Risk = "review"
	RiskCareful Risk = "careful"
)

// Size is a byte count. Units are 1024-based so numbers match `du -h`.
type Size int64

var sizeUnits = []string{"B", "KB", "MB", "GB", "TB"}

func (s Size) String() string {
	v, i := float64(s), 0
	for v >= 1024 && i < len(sizeUnits)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", int64(s))
	}
	return fmt.Sprintf("%.1f %s", v, sizeUnits[i])
}

// ParseSize parses "100MB", "1.5 gb" or a plain byte count.
func ParseSize(in string) (Size, error) {
	s := strings.TrimSpace(in)
	i := strings.IndexFunc(s, unicode.IsLetter)
	num, unit := s, "B"
	if i >= 0 {
		num, unit = strings.TrimSpace(s[:i]), strings.ToUpper(s[i:])
	}
	n, err := strconv.ParseFloat(num, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", in)
	}
	for p, u := range sizeUnits {
		if u == unit {
			mult := float64(1)
			for range p {
				mult *= 1024
			}
			return Size(n * mult), nil
		}
	}
	return 0, fmt.Errorf("invalid size unit in %q", in)
}

type Finding struct {
	Tool     string
	Label    string
	Path     string
	Size     Size
	Category Category
	Risk     Risk
	LastUsed time.Time
	Detector string
	// CleanVia, when set, is a command (argv) that removes this item instead of
	// deleting files, e.g. {"ollama", "rm", "llama3"}. Path is then informational.
	CleanVia []string
}

// Detector finds waste for one tool or cache type. Scan must be read-only.
type Detector interface {
	Name() string
	Scan(ctx context.Context, env Env) ([]Finding, error)
}

// Env is everything a detector may know about the machine; tests inject fakes.
type Env struct {
	Home   string
	OS     string
	Getenv func(string) string
	// Run executes a command and returns trimmed stdout.
	Run func(ctx context.Context, name string, args ...string) (string, error)
}

// NewEnv returns an Env backed by the real machine.
func NewEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home:   home,
		OS:     runtime.GOOS,
		Getenv: os.Getenv,
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, name, args...).Output()
			return strings.TrimSpace(string(out)), err
		},
	}, nil
}
