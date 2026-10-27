package cmd

import (
	"fmt"
	"runtime"

	"github.com/NiharRan/unclog/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and platform",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "unclog %s (commit %s, built %s) %s/%s\n",
				version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
		},
	}
}
