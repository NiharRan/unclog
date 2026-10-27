package safety

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
)

func testEnv(goos, home string, vars map[string]string) engine.Env {
	return engine.Env{Home: home, OS: goos, Getenv: func(k string) string { return vars[k] }}
}

var (
	linux   = testEnv("linux", "/home/u", map[string]string{"TMPDIR": "/tmp"})
	mac     = testEnv("darwin", "/Users/u", map[string]string{"TMPDIR": "/var/folders/ab/T/"})
	windows = testEnv("windows", `C:\Users\u`, map[string]string{"TEMP": `C:\Users\u\AppData\Local\Temp`})
)

func TestIsDenied(t *testing.T) {
	cases := []struct {
		env  engine.Env
		path string
		deny bool
	}{
		// root, home and ancestors
		{linux, "/", true}, {linux, "/home", true}, {linux, "/home/u", true},
		{mac, "/Users", true}, {mac, "/Users/u", true}, {windows, `C:\`, true}, {windows, `C:\Users\u`, true},
		// personal folders and anything inside
		{linux, "/home/u/Documents", true}, {linux, "/home/u/Documents/x", true},
		{linux, "/home/u/Desktop/a", true}, {linux, "/home/u/Downloads", true},
		{mac, "/Users/U/documents/x", true}, {windows, `c:\users\U\Documents\x`, true},
		// git, env, keys
		{linux, "/home/u/repo/.git/objects", true}, {linux, "/home/u/proj/.env", true},
		{linux, "/home/u/proj/.env.local", true}, {linux, "/home/u/.ssh", true},
		{linux, "/home/u/.ssh/id_rsa", true}, {linux, "/home/u/.gnupg", true},
		{linux, "/home/u/keys/server.pem", true},
		// browser profiles
		{linux, "/home/u/.config/google-chrome/Default", true}, {linux, "/home/u/.mozilla/firefox", true},
		{mac, "/Users/u/Library/Application Support/Google/Chrome/Default", true},
		{windows, `C:\Users\u\AppData\Local\Google\Chrome\User Data`, true},
		// system folders
		{linux, "/etc/passwd", true}, {linux, "/usr/bin/go", true}, {linux, "/usr/share", true},
		{mac, "/Applications/Safari.app", true}, {windows, `C:\Windows\System32`, true},
		// temp roots themselves
		{linux, "/tmp", true}, {linux, "/var/tmp", true}, {mac, "/var/folders", true},
		// traversal and relative
		{linux, "/home/u/.cache/../../u/.ssh", true}, {linux, "relative/path", true}, {linux, "", true},

		// fair game
		{linux, "/home/u/.cache/pip", false}, {linux, "/home/u/.npm/_cacache", false},
		{linux, "/tmp/foo", false}, {linux, "/var/tmp/x", false},
		{linux, "/usr/share/ollama/.ollama/models", false},
		{mac, "/Users/u/Library/Caches/pip", false}, {mac, "/var/folders/ab/T/x", false},
		{windows, `C:\Users\u\AppData\Local\npm-cache`, false},
	}
	for _, c := range cases {
		got, why := IsDenied(c.path, c.env)
		if got != c.deny {
			t.Errorf("IsDenied(%q, %s) = %v (%s); want %v", c.path, c.env.OS, got, why, c.deny)
		}
	}
}

func TestIsDeniedEmptyHome(t *testing.T) {
	if got, _ := IsDenied("/home/u/.cache/pip", testEnv("linux", "", nil)); !got {
		t.Error("unknown home must deny everything")
	}
}

func TestIsProtected(t *testing.T) {
	for p, want := range map[string]bool{
		"/home/u/.claude.json": true, "/home/u/.claude/settings.json": true,
		"/x/settings.local.json": true, "/x/Credentials.json": true, "/x/credentials": true,
		"/x/my_token_cache": true, "/x/auth.json": true,
		"/home/u/.cache/index-abc": false, "/x/settings.json.bak": false,
	} {
		if got, _ := IsProtected(p, linux); got != want {
			t.Errorf("IsProtected(%q) = %v; want %v", p, got, want)
		}
	}
}

func TestResolveWithin(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithin(filepath.Join(root, "ok"), root); err != nil {
		t.Errorf("inside path refused: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := ResolveWithin(filepath.Join(root, "link"), root); err == nil {
		t.Error("symlink escaping root must be refused")
	}
	if _, err := ResolveWithin(filepath.Join(root, "missing"), root); err == nil {
		t.Error("missing path must error")
	}
}

func newChecker(home string, open ...string) *Checker {
	env := testEnv("linux", home, nil)
	return &Checker{Env: env, Open: func() []string { return open }, Procs: func() []string { return nil }}
}

func TestValidate(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, ".cache")
	for _, d := range []string{filepath.Join(cache, "pip"), filepath.Join(home, "Documents")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cache, "pip", "wheel"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := func(p string) engine.Finding { return engine.Finding{Path: p} }

	if err := newChecker(home).Validate(f(filepath.Join(cache, "pip")), cache); err != nil {
		t.Errorf("good finding refused: %v", err)
	}

	// symlink to ~/Documents inside the cache root
	if err := os.Symlink(filepath.Join(home, "Documents"), filepath.Join(cache, "link")); err == nil {
		err := newChecker(home).Validate(f(filepath.Join(cache, "link")), cache)
		if !errors.Is(err, ErrRefused) {
			t.Errorf("symlink to Documents: err = %v; want ErrRefused", err)
		}
	}

	// path traversal back into Documents
	err := newChecker(home).Validate(f(filepath.Join(cache, "..", "Documents")), cache)
	if !errors.Is(err, ErrRefused) {
		t.Errorf("traversal: err = %v; want ErrRefused", err)
	}

	// protected file
	prot := filepath.Join(cache, "pip", "settings.json")
	_ = os.WriteFile(prot, nil, 0o644)
	if err := newChecker(home).Validate(f(prot), cache); !errors.Is(err, ErrRefused) {
		t.Errorf("protected file: err = %v; want ErrRefused", err)
	}

	// open file inside the directory (resolve first: t.TempDir may sit behind a symlink)
	real, _ := filepath.EvalSymlinks(filepath.Join(cache, "pip", "wheel"))
	err = newChecker(home, real).Validate(f(filepath.Join(cache, "pip")), cache)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "in use") {
		t.Errorf("open file: err = %v; want in-use refusal", err)
	}
}

func TestToolRunning(t *testing.T) {
	c := &Checker{Procs: func() []string { return []string{"Cursor Helper", "bash", "Cursorify"} }}
	if !c.ToolRunning("cursor") {
		t.Error("Cursor Helper should match cursor")
	}
	if c.ToolRunning("code") || c.ToolRunning("cursori") {
		t.Error("prefix of a longer word must not match")
	}
	c.Procs = func() []string { return []string{"Cursorify"} }
	if c.ToolRunning("cursor") {
		t.Error("Cursorify must not match cursor")
	}
}

func TestHistory(t *testing.T) {
	cfg := t.TempDir()
	h := NewHistory(testEnv("linux", "/home/u", map[string]string{"XDG_CONFIG_HOME": cfg}))
	if want := filepath.Join(cfg, "aiclean", "history.log"); h.Path() != want {
		t.Fatalf("path = %q; want %q", h.Path(), want)
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Append(Entry{Time: time.Now(), Action: "trash", Target: "/c/a\tb\n" + string(rune('a'+i%26)), Size: 42, Outcome: "ok"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	b, err := os.ReadFile(h.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("got %d lines; want 50", len(lines))
	}
	for _, l := range lines {
		if len(strings.Split(l, "\t")) != 5 {
			t.Fatalf("malformed line %q", l)
		}
	}
}

func TestHistoryUnwritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, nil, 0o644)
	h := NewHistory(testEnv("linux", "/home/u", map[string]string{"XDG_CONFIG_HOME": file}))
	if err := h.Append(Entry{Time: time.Now(), Action: "kill"}); err == nil {
		t.Fatal("append must return an error when the log cannot be written")
	}
}
