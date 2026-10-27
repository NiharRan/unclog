// Package cmd holds the cobra commands. Keep it thin: logic belongs in internal/engine.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// Exit codes shared by all commands.
const (
	ExitOK        = 0
	ExitError     = 1
	ExitNothing   = 2
	ExitCancelled = 3
)

// exitError makes a command exit with a specific code; msg, if set, is printed to stderr.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

var (
	quiet   bool
	noColor bool
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "unclog",
		Short:         "Find and clean RAM, disk and process waste left by AI coding tools",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&quiet, "quiet", false, "hide footers and non-essential output")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	root.AddCommand(newVersionCmd(), newScanCmd(), newPSCmd(), newCleanCmd(), newKillCmd(), newDoctorCmd(), newConfigCmd())
	return root
}

// ColorEnabled reports whether colored output is allowed: off with --no-color,
// NO_COLOR, or when stdout is not a terminal.
func ColorEnabled() bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(os.Stdout)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				_, _ = fmt.Fprintln(os.Stderr, ee.msg)
			}
			return ee.code
		}
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		return ExitError
	}
	return ExitOK
}
