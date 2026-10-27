// Package cmd holds the cobra commands. Keep it thin: logic belongs in internal/engine.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes shared by all commands.
const (
	ExitOK        = 0
	ExitError     = 1
	ExitNothing   = 2
	ExitCancelled = 3
)

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
	root.AddCommand(newVersionCmd())
	return root
}

// ColorEnabled reports whether colored output is allowed: off with --no-color,
// NO_COLOR, or when stdout is not a terminal.
func ColorEnabled() bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		return ExitError
	}
	return ExitOK
}
