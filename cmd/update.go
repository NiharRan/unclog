package cmd

import (
	"fmt"
	"os"

	"github.com/NiharRan/unclog/internal/config"
	"github.com/NiharRan/unclog/internal/update"
	"github.com/NiharRan/unclog/internal/version"
	"github.com/spf13/cobra"
)

// Overridable in tests.
var (
	stderrIsTerminal = func() bool { return isTerminal(os.Stderr) }
	exePath          = func() string { p, _ := os.Executable(); return p }
)

func updateNotice(latest string) string {
	return fmt.Sprintf("A new unclog %s is available (you have %s). Update: %s", latest, version.Version, update.UpgradeCommand(exePath()))
}

// notifyUpdate prints a one-line hint on stderr after a command when the user turned on
// update_check. It stays silent when piped, quiet, offline or up to date.
func notifyUpdate(cmd *cobra.Command) {
	switch cmd.Name() {
	case "version", "help", "completion":
		return
	}
	env, err := newEnv()
	if err != nil {
		return
	}
	cfg, err := config.Load(env)
	if err != nil || !cfg.UpdateCheck || isQuiet(cfg) || !stderrIsTerminal() {
		return
	}
	if latest := update.Latest(cmd.Context(), env, false); update.Newer(version.Version, latest) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "\n"+updateNotice(latest))
	}
}

// checkUpdate is `version --check`: asking explicitly is consent, so it ignores update_check.
func checkUpdate(cmd *cobra.Command) error {
	env, err := newEnv()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	latest := update.Latest(cmd.Context(), env, true)
	switch {
	case latest == "":
		_, _ = fmt.Fprintln(out, "Could not check for updates.")
	case update.Newer(version.Version, latest):
		_, _ = fmt.Fprintln(out, updateNotice(latest))
	default:
		_, _ = fmt.Fprintf(out, "unclog %s is up to date.\n", version.Version)
	}
	return nil
}
