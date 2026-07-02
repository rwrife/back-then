package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
)

// nearJSON is the stable shape emitted by `back-then near <file> --json`.
type nearJSON struct {
	Target  string           `json:"target"`
	Window  string           `json:"window"`
	Results []nearResultJSON `json:"results"`
}

type nearResultJSON struct {
	Path    string `json:"path"`
	When    string `json:"when"`
	OffsetS int64  `json:"offset_seconds"`
	Size    int64  `json:"size_bytes"`
}

// newNearCmd returns the `back-then near <file>` subcommand — the episodic
// payoff. Given a file you remember, it surfaces the other files that arrived
// around the same time (the rest of that "session"), ordered by proximity.
func newNearCmd(dbPath *string) *cobra.Command {
	var asJSON bool
	var limit int
	var window time.Duration

	cmd := &cobra.Command{
		Use:   "near <file>",
		Short: "Show files that arrived around the same time as <file>",
		Long: `Given a file you do remember, show the other files that arrived or changed
around the same time — the rest of that "session." Results are ordered by how
close in time they are to the target.

The file must already be in the index (run ` + "`back-then index <path>`" + `
first). Use --window to widen or narrow how far out to look.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := defaultDBPath(*dbPath)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}

			// Match the index's absolute-path convention so a relative
			// argument still resolves against the stored records.
			target, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolve target path: %w", err)
			}

			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			rec, ok, err := st.FileByPath(target)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%s is not in the index; run `back-then index <path>` that covers it first", target)
			}

			files, err := st.AllFiles()
			if err != nil {
				return err
			}

			results := sessions.Near(files, target, window)
			if limit > 0 && len(results) > limit {
				results = results[:limit]
			}

			out := cmd.OutOrStdout()

			if asJSON {
				nj := nearJSON{Target: target, Window: window.String()}
				for _, r := range results {
					nj.Results = append(nj.Results, nearResultJSON{
						Path:    r.File.Path,
						When:    r.File.EffectiveTime().Format(time.RFC3339),
						OffsetS: int64(r.Delta / time.Second),
						Size:    r.File.Size,
					})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(nj)
			}

			if err := render.Line(out, fmt.Sprintf("Around %s (%s), within %s:",
				target, render.Date(rec.EffectiveTime()), window)); err != nil {
				return err
			}
			if len(results) == 0 {
				return render.Line(out, "  No other files arrived in that window.")
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(tw, "OFFSET\tWHEN\tSIZE\tPATH"); err != nil {
				return err
			}
			for _, r := range results {
				if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					offsetHuman(r.Delta),
					r.File.EffectiveTime().Format("2006-01-02 15:04"),
					render.Bytes(r.File.Size),
					r.File.Path); err != nil {
					return err
				}
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of files to show (0 = all)")
	cmd.Flags().DurationVar(&window, "window", 6*time.Hour, "how far before/after the target to look (e.g. 30m, 2h, 24h)")

	return cmd
}

// offsetHuman renders a signed duration as a short relative label, e.g.
// "+12m", "-3h", "now" for near-zero.
func offsetHuman(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%s%dm", sign, int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%s%dh%02dm", sign, int(d/time.Hour), int((d%time.Hour)/time.Minute))
	default:
		return fmt.Sprintf("%s%dd%02dh", sign, int(d/(24*time.Hour)), int((d%(24*time.Hour))/time.Hour))
	}
}
