package engine

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemInfo is system memory plus a coarse pressure level: normal, warning or critical.
type MemInfo struct {
	Total, Used, Available uint64
	Pressure               string
}

// ReadMem reads system memory. Pressure comes from the OS where it exposes one
// (macOS memorystatus level, Linux PSI) and from used-percent elsewhere.
// shortcut: Windows uses used-percent, not commit charge; upgrade if that misleads.
func ReadMem(ctx context.Context, env Env) MemInfo {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return MemInfo{}
	}
	info := MemInfo{Total: vm.Total, Used: vm.Used, Available: vm.Available, Pressure: pressureFromPercent(vm.UsedPercent)}
	switch env.OS {
	case "darwin":
		if env.Run != nil {
			if out, err := env.Run(ctx, "sysctl", "-n", "kern.memorystatus_vm_pressure_level"); err == nil {
				if p := darwinPressure(out); p != "" {
					info.Pressure = p
				}
			}
		}
	case "linux":
		if b, err := os.ReadFile("/proc/pressure/memory"); err == nil {
			if p := psiPressure(string(b)); p != "" {
				info.Pressure = p
			}
		}
	}
	return info
}

func pressureFromPercent(p float64) string {
	switch {
	case p >= 92:
		return "critical"
	case p >= 80:
		return "warning"
	}
	return "normal"
}

// darwinPressure maps kern.memorystatus_vm_pressure_level (1 normal, 2 warn, 4 critical).
func darwinPressure(level string) string {
	switch strings.TrimSpace(level) {
	case "1":
		return "normal"
	case "2":
		return "warning"
	case "4":
		return "critical"
	}
	return ""
}

// psiPressure reads the "some avg10" stall percentage from /proc/pressure/memory.
func psiPressure(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(f, "avg10="); ok {
				n, err := strconv.ParseFloat(v, 64)
				switch {
				case err != nil:
					return ""
				case n >= 40:
					return "critical"
				case n >= 10:
					return "warning"
				}
				return "normal"
			}
		}
	}
	return ""
}
