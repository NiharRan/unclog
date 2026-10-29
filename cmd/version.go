package cmd

import (
	"fmt"
	"runtime"

	"github.com/NiharRan/unclog/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print version and platform",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "unclog %s (commit %s, built %s) %s/%s\n",
				version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
			if check {
				return checkUpdate(cmd)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "ask GitHub whether a newer release exists (the only network call)")
	return c
}
