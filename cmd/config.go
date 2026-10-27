package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NiharRan/unclog/internal/config"
	"github.com/spf13/cobra"
)

// runEditor opens file in the user's editor; overridable in tests.
var runEditor = func(editor, file string) error {
	fields := strings.Fields(editor)
	c := exec.Command(fields[0], append(fields[1:], file)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			cfg, err := loadConfig(cmd, env)
			if err != nil {
				return err
			}
			state := "not found, using defaults"
			if cfg.Exists {
				state = "loaded"
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "# %s (%s)\n%s", cfg.File, state, cfg.YAML())
			return err
		},
	}
	c.AddCommand(configInit(), configEdit(), configPath())
	return c
}

func configPath() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), config.Path(env))
			return err
		},
	}
}

func configInit() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write a commented default config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			file := config.Path(env)
			if err := config.Write(file); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Wrote", file)
			return err
		},
	}
}

func configEdit() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config file in $VISUAL or $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv()
			if err != nil {
				return err
			}
			file := config.Path(env)
			editor := env.Getenv("VISUAL")
			if editor == "" {
				editor = env.Getenv("EDITOR")
			}
			if strings.TrimSpace(editor) == "" {
				return fmt.Errorf("set $VISUAL or $EDITOR to edit the config; the file is at %s", file)
			}
			if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
				if err := config.Write(file); err != nil {
					return err
				}
			}
			if err := runEditor(editor, file); err != nil {
				return fmt.Errorf("editor: %w", err)
			}
			// Report mistakes right away instead of at the next scan.
			if _, err := config.Load(env); err != nil {
				return fmt.Errorf("saved, but the config is invalid: %w", err)
			}
			return nil
		},
	}
}
