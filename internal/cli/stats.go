package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/store"
)

// statsJSON is the stable shape emitted by `back-then stats --json`. Times are
// RFC3339 strings (empty when the index is empty) for easy downstream parsing.
type statsJSON struct {
	Files     int    `json:"files"`
	TotalSize int64  `json:"total_size_bytes"`
	Oldest    string `json:"oldest,omitempty"`
	Newest    string `json:"newest,omitempty"`
	TopExts   []struct {
		Ext   string `json:"ext"`
		Count int    `json:"count"`
	} `json:"top_exts"`
}

// newStatsCmd returns the `back-then stats` subcommand, which prints a summary
// of the current index: file count, total size, the span of modified times,
// and the most common extensions.
func newStatsCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var asJSON bool
	var topN int

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize the current index",
		Long: `Print a summary of the local index: how many files are recorded,
their total size, the span between the oldest and newest modified times, and
the most common file extensions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveDBPath(*dbPath, cfg.DB)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}

			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			s, err := st.Stats(topN)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				js := statsJSON{Files: s.Files, TotalSize: s.TotalSize}
				if !s.Oldest.IsZero() {
					js.Oldest = s.Oldest.Format("2006-01-02T15:04:05Z07:00")
				}
				if !s.Newest.IsZero() {
					js.Newest = s.Newest.Format("2006-01-02T15:04:05Z07:00")
				}
				for _, e := range s.TopExts {
					js.TopExts = append(js.TopExts, struct {
						Ext   string `json:"ext"`
						Count int    `json:"count"`
					}{e.Ext, e.Count})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(js)
			}

			if s.Files == 0 {
				return render.Line(out, "Index is empty. Run `back-then index <path>` to populate it.")
			}

			if err := render.Line(out, fmt.Sprintf("Files:      %d", s.Files)); err != nil {
				return err
			}
			if err := render.Line(out, fmt.Sprintf("Total size: %s", render.Bytes(s.TotalSize))); err != nil {
				return err
			}
			if err := render.Line(out, fmt.Sprintf("Date span:  %s \u2192 %s", render.Date(s.Oldest), render.Date(s.Newest))); err != nil {
				return err
			}
			if len(s.TopExts) == 0 {
				return nil
			}
			if err := render.Line(out, "Top extensions:"); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, e := range s.TopExts {
				if _, err := fmt.Fprintf(tw, "  %s\t%d\n", e.Ext, e.Count); err != nil {
					return err
				}
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().IntVar(&topN, "top", 10, "number of extensions to list")

	return cmd
}
