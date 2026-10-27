// Package safety decides whether a path may ever be deleted and records every action.
// Deny wins over everything: detector output, --yes, user selection.
package safety

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/NiharRan/unclog/internal/paths"
)

var (
	driveAbs  = regexp.MustCompile(`(?i)^[a-z]:(/|$)`)
	driveRoot = regexp.MustCompile(`(?i)^[a-z]:$`)
)

// norm returns a cleaned, slash-separated form; lowercased on case-insensitive OSes.
// Backslashes always count as separators so Windows paths are judged the same on any host.
func norm(p string, env engine.Env) string {
	p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if env.OS == "darwin" || env.OS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func under(p, base string) bool {
	return p == base || strings.HasPrefix(p, strings.TrimSuffix(base, "/")+"/")
}

// withResolved adds the symlink-resolved form of each existing path, so a Home
// or TMPDIR reached through a symlink (macOS /var -> /private/var) is still matched.
func withResolved(in ...string) []string {
	var out []string
	for _, p := range in {
		if p == "" {
			continue
		}
		out = append(out, p)
		if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
			out = append(out, r)
		}
	}
	return out
}

// Directories where nothing inside, and no ancestor, may be deleted.
var systemTrees = []string{
	"/System", "/bin", "/sbin", "/etc", "/lib", "/lib32", "/lib64", "/boot", "/dev", "/proc", "/sys",
	"/Library", "/Applications", "/private/etc",
	"/usr/bin", "/usr/sbin", "/usr/lib", "/usr/lib64", "/usr/libexec", "/usr/include", "/usr/local",
	"C:/Windows", "C:/Program Files", "C:/Program Files (x86)", "C:/ProgramData",
}

// Home-relative trees.
var homeTrees = []string{"Documents", "Desktop", "Downloads", ".ssh", ".gnupg", ".mozilla", "Library/Safari", "snap/firefox"}

// Browser profile folders, relative to the app-support, config and cache roots.
var browserDirs = []string{
	"Google/Chrome", "google-chrome", "chromium", "Chromium", "Microsoft Edge", "microsoft-edge",
	"Microsoft/Edge", "BraveSoftware", "Firefox", "Mozilla", "Arc", "Vivaldi", "vivaldi",
}

var keyFileNames = map[string]bool{"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true}
var keyFileExts = map[string]bool{".pem": true, ".key": true, ".p12": true, ".pfx": true}

// IsDenied reports whether path must never be deleted, and why.
func IsDenied(p string, env engine.Env) (bool, string) {
	n := norm(p, env)
	switch {
	case env.Home == "":
		return true, "home directory unknown"
	case !strings.HasPrefix(n, "/") && !driveAbs.MatchString(n):
		return true, "path is not absolute"
	case n == "/" || driveRoot.MatchString(n):
		return true, "filesystem root"
	}

	for _, c := range strings.Split(n, "/") {
		if c == ".git" {
			return true, "inside a .git folder"
		}
		if c == ".env" || strings.HasPrefix(c, ".env.") {
			return true, ".env file"
		}
	}
	if b := path.Base(n); keyFileNames[b] || keyFileExts[path.Ext(b)] {
		return true, "private key file"
	}

	// Equal to, inside, or an ancestor of these.
	var trees []string
	trees = append(trees, systemTrees...)
	homes := withResolved(env.Home)
	for _, h := range homes {
		for _, t := range homeTrees {
			trees = append(trees, h+"/"+t)
		}
	}
	for _, base := range withResolved(paths.AppSupportDir(env), paths.ConfigDir(env), paths.CacheDir(env)) {
		for _, b := range browserDirs {
			trees = append(trees, base+"/"+b)
		}
	}
	for _, t := range trees {
		t = norm(t, env)
		if under(n, t) || under(t, n) {
			return true, "protected location " + t
		}
	}

	// Equal to or an ancestor of these, but their children are fair game.
	keeps := append(withResolved(env.Home), withResolved(paths.TempDirs(env)...)...)
	keeps = append(keeps, "/usr/share", "/var", "/private/var")
	for _, k := range keeps {
		if k = norm(k, env); under(k, n) {
			return true, "would remove " + k
		}
	}
	return false, ""
}

// IsProtected reports whether path names a config or auth file that is never touched.
func IsProtected(p string, env engine.Env) (bool, string) {
	b := strings.ToLower(path.Base(norm(p, env)))
	switch {
	case b == ".claude.json" || b == "settings.json" || b == "settings.local.json" || b == "auth.json",
		strings.HasPrefix(b, "credentials"), strings.Contains(b, "token"):
		return true, "config or auth file"
	}
	return false, ""
}
