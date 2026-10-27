package safety

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/NiharRan/unclog/internal/engine"
	"github.com/shirou/gopsutil/v4/process"
)

// listOpenFiles returns absolute paths open by running processes.
// shortcut: Windows has no listing; Checker.IsOpen probes single files instead. Upgrade if directories need checking there.
func listOpenFiles(env engine.Env) []string {
	switch env.OS {
	case "windows":
		return nil
	case "darwin":
		// One lsof call; gopsutil would spawn one per process.
		out, _ := env.Run(context.Background(), "lsof", "-w", "-n", "-F", "n")
		var files []string
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "n/") {
				files = append(files, l[1:])
			}
		}
		return files
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	procs, _ := process.ProcessesWithContext(ctx)
	var files []string
	for _, p := range procs {
		of, _ := p.OpenFilesWithContext(ctx)
		for _, f := range of {
			if strings.HasPrefix(f.Path, "/") {
				files = append(files, f.Path)
			}
		}
	}
	return files
}

func listProcessNames() []string {
	procs, _ := process.Processes()
	var names []string
	for _, p := range procs {
		if n, err := p.Name(); err == nil {
			names = append(names, n)
		}
	}
	return names
}

// IsOpen reports whether path, or anything under it, is open by a running process.
func (c *Checker) IsOpen(p string) bool {
	if c.Env.OS == "windows" && runtime.GOOS == "windows" {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			f, err := os.OpenFile(p, os.O_RDWR, 0)
			if err != nil {
				return true
			}
			_ = f.Close()
		}
		return false
	}
	c.once.Do(func() { c.open = c.Open() })
	n := norm(p, c.Env)
	for _, f := range c.open {
		if under(norm(f, c.Env), n) {
			return true
		}
	}
	return false
}

// ToolRunning reports whether any process is named like one of names
// ("Cursor" matches "Cursor" and "Cursor Helper", not "Cursorify").
func (c *Checker) ToolRunning(names ...string) bool {
	for _, proc := range c.Procs() {
		pn := strings.TrimSuffix(strings.ToLower(proc), ".exe")
		for _, n := range names {
			n = strings.ToLower(n)
			if pn == n || strings.HasPrefix(pn, n+" ") {
				return true
			}
		}
	}
	return false
}
