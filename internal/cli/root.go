// Package cli wires up the back-then command-line interface using cobra.
//
// The root command and its subcommands live here so that main() stays a thin
// entrypoint and the command tree is easy to unit-test.
package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
)

// rootLong is the long description shown by `back-then --help`.
const rootLong = `back-then is a local-first time machine for your filesystem.

Instead of searching by a filename you don't remember, you search by roughly
when a file showed up and what was going on around it: "that spreadsheet from
around tax season," "the photo from the week of the move." It reads only
on-disk signals (timestamps, EXIF capture dates, the burst of files that
arrived together, the folder they lived in) and ranks candidates.

100% offline. No cloud, no account, no telemetry. Your files never leave the
machine.`

// NewRootCmd builds the root cobra command with all subcommands attached.
// It is a constructor (rather than a package-level var) so tests can build a
// fresh, isolated command tree per case.
func NewRootCmd() *cobra.Command {
	// dbPath holds the resolved index location, settable via the persistent
	// --db flag and defaulting to a per-user data location.
	var dbPath string

	// cfg holds user defaults loaded from the optional config file. A missing
	// or unreadable file yields the zero Config so the CLI still works; we
	// stash any load error and surface it on the first command that runs, so a
	// typo in the file is loud rather than silent.
	cfg, cfgErr := loadConfig()

	root := &cobra.Command{
		Use:   "back-then",
		Short: "A local-first time machine for your files",
		Long:  rootLong,
		// We define our own subcommands; silence cobra's usage/error spew so
		// callers (main) control exit behavior.
		SilenceUsage:  true,
		SilenceErrors: true,
		// Running the bare command with no subcommand prints help rather than
		// erroring out.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.PersistentFlags().StringVar(&dbPath, "db", "", "path to the index database (default: config or per-user data dir)")

	// Surface a config-load failure once, before any subcommand does work.
	if cfgErr != nil {
		root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			return cfgErr
		}
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newIndexCmd(&dbPath, cfg))
	root.AddCommand(newWatchCmd(&dbPath, cfg))
	root.AddCommand(newStatsCmd(&dbPath, cfg))
	root.AddCommand(newFindCmd(&dbPath, cfg))
	root.AddCommand(newSessionsCmd(&dbPath, cfg))
	root.AddCommand(newTimelineCmd(&dbPath, cfg))
	root.AddCommand(newTagCmd(&dbPath, cfg))
	root.AddCommand(newNearCmd(&dbPath, cfg))
	root.AddCommand(newDupesCmd(&dbPath, cfg))
	root.AddCommand(newForgetCmd(&dbPath, cfg))
	root.AddCommand(newConfigCmd(cfg))

	return root
}

// configEnvVar overrides the config file location, primarily for tests and
// power users who keep multiple profiles.
const configEnvVar = "BACK_THEN_CONFIG"

// loadConfig resolves the config path (honoring BACK_THEN_CONFIG) and loads it.
// A missing file is not an error; a malformed one is.
func loadConfig() (config.Config, error) {
	path := os.Getenv(configEnvVar)
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			// No config dir on this OS: proceed with built-in defaults.
			return config.Config{}, nil
		}
		path = p
	}
	return config.Load(path)
}

// defaultDBPath returns the resolved index path: the --db value when set,
// otherwise the config's db value, otherwise
// <user-config-dir>/back-then/index.db. It ensures the parent directory exists
// so callers can open the database directly.
func defaultDBPath(flagVal string) (string, error) {
	return resolveDBPath(flagVal, "")
}

// resolveDBPath is defaultDBPath with an explicit config fallback, letting
// commands thread their loaded config in without a package global.
func resolveDBPath(flagVal, cfgDB string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if cfgDB != "" {
		if err := os.MkdirAll(filepath.Dir(cfgDB), 0o755); err != nil {
			return "", err
		}
		return cfgDB, nil
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		// Fall back to the working directory if the OS has no config dir.
		base = "."
	}
	dir := filepath.Join(base, "back-then")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "index.db"), nil
}

// effectiveLimit returns the config-provided result limit when the file set
// one, otherwise the command's built-in fallback. This makes a configured
// "limit" the default that an explicit --limit flag can still override.
func effectiveLimit(cfg config.Config, fallback int) int {
	if cfg.LimitSet {
		return cfg.Limit
	}
	return fallback
}

// effectiveGap returns the config-provided session gap when set (> 0),
// otherwise the built-in fallback.
func effectiveGap(cfg config.Config, fallback time.Duration) time.Duration {
	if cfg.Gap > 0 {
		return cfg.Gap
	}
	return fallback
}
