// Package config loads ~/.config/unclog/config.yaml: exclusions, custom paths and thresholds.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
	"github.com/NiharRan/unclog/internal/safety"
	"github.com/spf13/viper"
)

const (
	DefaultIdleMinutes = 10
	maxIdleMinutes     = 24 * 60
)

// Config is the validated configuration. The zero value plus Default() is a working setup.
type Config struct {
	Exclude           []string    // paths or globs that are never listed or removed
	CustomPaths       []string    // extra directories reported as findings
	MinSize           engine.Size // hide findings smaller than this
	OrphanIdleMinutes int         // near-zero CPU for this long makes an orphan
	Quiet             bool
	UpdateCheck       bool // opt-in: hint on stderr when a newer release exists (one request a day)

	// Warnings lists non-fatal problems such as unknown keys.
	Warnings []string
	// File is the config path and Exists whether it was read.
	File   string
	Exists bool

	home string
	fold bool // case-insensitive path matching (macOS, Windows)
	win  bool
}

var knownKeys = []string{"exclude", "custom_paths", "min_size", "orphan_idle_minutes", "quiet", "update_check"}

func Default() Config { return Config{OrphanIdleMinutes: DefaultIdleMinutes} }

// Path returns where the config file lives (XDG_CONFIG_HOME or %APPDATA% aware).
func Path(env engine.Env) string { return filepath.Join(paths.ConfigDir(env), "unclog", "config.yaml") }

// Load reads the config file. A missing or empty file yields the defaults.
func Load(env engine.Env) (Config, error) {
	c := Default()
	c.File, c.home = Path(env), env.Home
	c.fold, c.win = env.OS == "darwin" || env.OS == "windows", env.OS == "windows"

	v := viper.New()
	v.SetConfigFile(c.File)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if errors.As(err, &nf) || errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return c, fmt.Errorf("%s: %w", c.File, err)
	}
	c.Exists = true

	for _, k := range v.AllKeys() {
		top, _, _ := strings.Cut(k, ".")
		if !slices.Contains(knownKeys, top) {
			c.Warnings = append(c.Warnings, fmt.Sprintf("unknown key %q in %s", top, c.File))
		}
	}
	slices.Sort(c.Warnings)
	c.Warnings = slices.Compact(c.Warnings)

	var err error
	if c.Exclude, err = stringList(v, "exclude"); err != nil {
		return c, c.errf(err)
	}
	if c.CustomPaths, err = stringList(v, "custom_paths"); err != nil {
		return c, c.errf(err)
	}
	for i, p := range c.CustomPaths {
		c.CustomPaths[i] = c.expand(p)
		if !filepath.IsAbs(c.CustomPaths[i]) {
			return c, c.errf(fmt.Errorf("custom_paths: %q must be an absolute path or start with ~/", p))
		}
	}
	for i, p := range c.Exclude {
		c.Exclude[i] = c.expand(p)
	}
	if raw := v.Get("min_size"); raw != nil {
		if c.MinSize, err = parseMinSize(raw); err != nil {
			return c, c.errf(err)
		}
	}
	if raw := v.Get("orphan_idle_minutes"); raw != nil {
		n, ok := raw.(int)
		if !ok || n < 1 || n > maxIdleMinutes {
			return c, c.errf(fmt.Errorf("orphan_idle_minutes: must be a whole number from 1 to %d, got %v", maxIdleMinutes, raw))
		}
		c.OrphanIdleMinutes = n
	}
	if c.Quiet, err = boolKey(v, "quiet"); err != nil {
		return c, c.errf(err)
	}
	if c.UpdateCheck, err = boolKey(v, "update_check"); err != nil {
		return c, c.errf(err)
	}
	return c, nil
}

func (c Config) errf(err error) error { return fmt.Errorf("%s: %w", c.File, err) }

func stringList(v *viper.Viper, key string) ([]string, error) {
	raw := v.Get(key)
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a list of strings", key)
	}
	var out []string
	for _, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s: every entry must be a non-empty string, got %v", key, item)
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, nil
}

func boolKey(v *viper.Viper, key string) (bool, error) {
	raw := v.Get(key)
	if raw == nil {
		return false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s: must be true or false, got %v", key, raw)
	}
	return b, nil
}

func parseMinSize(raw any) (engine.Size, error) {
	switch x := raw.(type) {
	case string:
		s, err := engine.ParseSize(x)
		if err != nil {
			return 0, fmt.Errorf("min_size: %w", err)
		}
		return s, nil
	case int:
		if x < 0 {
			return 0, fmt.Errorf("min_size: must not be negative")
		}
		return engine.Size(x), nil
	}
	return 0, fmt.Errorf("min_size: must be a size like 100MB, got %v", raw)
}

// expand resolves a leading ~ to the home directory.
func (c Config) expand(p string) string {
	if p == "~" {
		return c.home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(c.home, rest)
	}
	return p
}

func (c Config) norm(p string) string {
	if c.win {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	p = path.Clean(filepath.ToSlash(p))
	if c.fold {
		p = strings.ToLower(p)
	}
	return p
}

// Excluded reports whether p is covered by an exclusion: p itself, anything inside an
// excluded directory, a glob match on p or one of its ancestors, or a directory that
// contains an excluded path (removing it would remove the excluded content).
// A pattern without a slash, like *.log, matches names at any depth.
func (c Config) Excluded(p string) bool {
	n := c.norm(p)
	for _, raw := range c.Exclude {
		pat := c.norm(raw)
		hasSep, isGlob := strings.Contains(pat, "/"), strings.ContainsAny(pat, "*?[")
		for a := n; ; a = path.Dir(a) {
			target := a
			if !hasSep {
				target = path.Base(a)
			}
			if ok, _ := path.Match(pat, target); ok {
				return true
			}
			if path.Dir(a) == a {
				break
			}
		}
		if hasSep && !isGlob && (pat == n || strings.HasPrefix(pat, strings.TrimSuffix(n, "/")+"/")) {
			return true
		}
	}
	return false
}

// Keep drops excluded findings; findings without a path (Docker) are unaffected.
func (c Config) Keep(f engine.Finding) bool { return f.Path == "" || !c.Excluded(f.Path) }

// guard refuses excluded paths at clean time, so an exclusion holds even if a finding slipped through.
type guard struct {
	inner engine.Guard
	cfg   Config
}

func (g guard) Validate(f engine.Finding, root string) error {
	if f.Path != "" && g.cfg.Excluded(f.Path) {
		return fmt.Errorf("%w %s: excluded by config", safety.ErrRefused, f.Path)
	}
	return g.inner.Validate(f, root)
}

// Guard wraps inner so excluded paths are refused.
func (c Config) Guard(inner engine.Guard) engine.Guard { return guard{inner, c} }

// YAML renders the effective settings.
func (c Config) YAML() string {
	var b strings.Builder
	list := func(key string, items []string) {
		if len(items) == 0 {
			fmt.Fprintf(&b, "%s: []\n", key)
			return
		}
		fmt.Fprintf(&b, "%s:\n", key)
		for _, it := range items {
			fmt.Fprintf(&b, "  - %q\n", it)
		}
	}
	list("exclude", c.Exclude)
	list("custom_paths", c.CustomPaths)
	fmt.Fprintf(&b, "min_size: %d\n", int64(c.MinSize))
	fmt.Fprintf(&b, "orphan_idle_minutes: %d\n", c.OrphanIdleMinutes)
	fmt.Fprintf(&b, "quiet: %v\n", c.Quiet)
	fmt.Fprintf(&b, "update_check: %v\n", c.UpdateCheck)
	return b.String()
}

// DefaultYAML is the commented file written by `config init`.
const DefaultYAML = `# unclog configuration. Every key is optional.

# Paths or globs that are never listed or removed. A path covers everything inside it.
# A pattern without a slash, like "*.log", matches names at any depth. "~/" is your home.
exclude: []
#  - ~/.cache/pip
#  - ~/work/*/node_modules

# Extra directories to report as findings (risk: review). Must be absolute or start with ~/.
custom_paths: []
#  - ~/.cache/my-ai-tool

# Hide findings smaller than this, e.g. 100MB. 0 shows everything.
min_size: 0

# A matched process counts as an orphan after this many minutes of near-zero CPU.
orphan_idle_minutes: 10

# Hide the waitlist footer and other non-essential output.
quiet: false

# Opt-in. When true, unclog asks GitHub at most once a day and prints a hint if a newer
# release exists. Off means unclog makes no network calls.
update_check: false
`

// Write creates the default config file. It refuses to overwrite an existing one.
func Write(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; use `unclog config edit`", file)
		}
		return err
	}
	if _, err := f.WriteString(DefaultYAML); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Keys lists the supported keys, for help text and tests.
func Keys() []string { k := slices.Clone(knownKeys); sort.Strings(k); return k }
