// Package update tells the user when a newer unclog release exists. It is the only
// network code in the program and only runs when the user opts in (see config update_check).
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

const (
	latestURL = "https://api.github.com/repos/NiharRan/unclog/releases/latest"
	ttl       = 24 * time.Hour
	timeout   = 2 * time.Second
)

type cache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// Fetch returns the latest release tag. Tests replace it.
var Fetch = func(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

// Latest returns the newest release tag, asking the network at most once a day unless
// force is set. A failed check returns "" and is not retried until the cache expires.
func Latest(ctx context.Context, env engine.Env, force bool) string {
	file := filepath.Join(paths.CacheDir(env), "unclog", "update.json")
	var c cache
	if b, err := os.ReadFile(file); err == nil && json.Unmarshal(b, &c) == nil && !force && time.Since(c.Checked) < ttl {
		return c.Latest
	}
	tag, err := Fetch(ctx)
	if err != nil {
		tag = ""
	}
	if b, err := json.Marshal(cache{Checked: time.Now(), Latest: tag}); err == nil {
		if os.MkdirAll(filepath.Dir(file), 0o700) == nil {
			_ = os.WriteFile(file, b, 0o600)
		}
	}
	return tag
}

// Newer reports whether latest is a higher release than current. Dev builds and
// anything that is not vMAJOR.MINOR.PATCH never count as outdated.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) (out [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// UpgradeCommand suggests how to update, judged from where the running binary lives.
func UpgradeCommand(exe string) string {
	low := strings.ToLower(strings.ReplaceAll(exe, `\`, "/"))
	switch {
	case strings.Contains(low, "/caskroom/") || strings.Contains(low, "/cellar/") || strings.Contains(low, "homebrew"):
		return "brew upgrade --cask unclog"
	case strings.Contains(low, "/scoop/"):
		return "scoop update unclog"
	}
	return "go install github.com/NiharRan/unclog@latest"
}
