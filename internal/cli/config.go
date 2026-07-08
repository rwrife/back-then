package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
)

// newConfigCmd returns the `back-then config` command group. It doesn't change
// any settings (the config file is edited by hand); it just helps users find
// and understand it: `config path` prints where the file lives and `config
// show` prints the effective values after defaults are applied.
//
// The already-loaded cfg is passed in so `show` reflects exactly what the rest
// of the CLI is using this run, rather than re-reading the file.
func newConfigCmd(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the config file location and effective settings",
		Long: `Inspect back-then's optional configuration.

back-then reads defaults from a JSON file so you don't have to retype flags.
The file is optional and hand-edited; this command only helps you locate and
review it — it does not write settings for you.

Recognized keys (all optional):

  db     string    where the index database lives
  roots  [string]  folders that bare ` + "`index`/`watch`" + ` scan
  gap    string    session split gap, e.g. "90m" or "3h"
  limit  number    default result cap for list commands (negative = no cap)

Set the file location with the BACK_THEN_CONFIG environment variable to keep
multiple profiles.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigShowCmd(cfg))

	return cmd
}

// newConfigPathCmd prints the resolved config file path and whether it exists,
// so a user knows exactly which file to create or edit.
func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the resolved config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := os.Getenv(configEnvVar)
			source := "default"
			if path != "" {
				source = configEnvVar
			} else {
				p, err := config.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}

			out := cmd.OutOrStdout()
			status := "not present (using built-in defaults)"
			if _, err := os.Stat(path); err == nil {
				status = "present"
			}
			return render.Line(out, fmt.Sprintf("%s  [%s, %s]", path, source, status))
		},
	}
}

// configShowJSON is the stable shape emitted by `config show --json`. Durations
// render as human strings to match the config file's own format.
type configShowJSON struct {
	DB    string   `json:"db"`
	Roots []string `json:"roots"`
	Gap   string   `json:"gap"`
	Limit int      `json:"limit"`
}

// newConfigShowCmd prints the effective configuration: what the CLI actually
// uses this run after config values and built-in defaults are merged.
func newConfigShowCmd(cfg config.Config) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration (config + defaults)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			db, err := resolveDBPath("", cfg.DB)
			if err != nil {
				return err
			}
			gap := effectiveGap(cfg, sessions.DefaultGap)
			limit := effectiveLimit(cfg, 20)

			if asJSON {
				js := configShowJSON{
					DB:    db,
					Roots: cfg.Roots,
					Gap:   gap.String(),
					Limit: limit,
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(js)
			}

			roots := "(none configured)"
			if len(cfg.Roots) > 0 {
				roots = strings.Join(cfg.Roots, ", ")
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "db\t%s\n", db)
			fmt.Fprintf(tw, "roots\t%s\n", roots)
			fmt.Fprintf(tw, "gap\t%s\n", gap)
			fmt.Fprintf(tw, "limit\t%d\n", limit)
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")

	return cmd
}
