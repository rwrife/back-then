package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/rank"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/when"
)

// findResultJSON is the stable shape emitted by `back-then find --json` for
// each result. Times are RFC3339 strings; score is the [0,1] match score.
type findResultJSON struct {
	Path      string  `json:"path"`
	Size      int64   `json:"size_bytes"`
	ModTime   string  `json:"mod_time"`
	Ext       string  `json:"ext"`
	ParentDir string  `json:"parent_dir"`
	Score     float64 `json:"score"`
}

// findJSON wraps the result list with the resolved query window so scripts can
// see how the fuzzy phrase was interpreted.
type findJSON struct {
	Query  string `json:"query"`
	Window struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"window"`
	Count   int              `json:"count"`
	Results []findResultJSON `json:"results"`
}

// newFindCmd returns the `back-then find "<query>"` subcommand: it turns a
// fuzzy time phrase into a window, pulls candidate files from the index, ranks
// them by time proximity, and prints the top matches.
func newFindCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var asJSON bool
	var limit int

	cmd := &cobra.Command{
		Use:   `find "<time phrase>"`,
		Short: "Find files by roughly when they showed up",
		Long: `Search the index by a fuzzy time phrase rather than a filename.

The phrase is resolved into a date window and files are ranked by how close
their timestamp sits to it. Examples:

  back-then find "last spring"
  back-then find "around march"
  back-then find "3 weeks ago"
  back-then find "december 2024"
  back-then find "2018"
  back-then find "2024-03-15"

Results are ordered best-match first. Use --json for scripting and --limit to
change how many results are shown.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			now := time.Now()

			path, err := resolveDBPath(*dbPath, cfg.DB)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}
			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			w, err := when.Parse(query, now)
			if err != nil {
				// Not a time phrase? It may be a session label. Fall back to a
				// label lookup so `find "Berlin trip"` jumps to the tagged
				// window. Only surface the parse error when no label matches.
				labels, lerr := st.LabelByName(query)
				if lerr != nil {
					return lerr
				}
				if len(labels) == 0 {
					return err
				}
				w = labelWindow(labels)
			}

			cands, err := st.Candidates(w, 0)
			if err != nil {
				return err
			}
			ranked := rank.Rank(cands, w)
			if limit > 0 && len(ranked) > limit {
				ranked = ranked[:limit]
			}

			out := cmd.OutOrStdout()
			if asJSON {
				return emitFindJSON(out, query, w, ranked)
			}
			return emitFindTable(out, w, ranked)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().IntVar(&limit, "limit", effectiveLimit(cfg, 20), "maximum number of results to show")

	return cmd
}

func emitFindJSON(out io.Writer, query string, w when.Window, ranked []rank.Candidate) error {
	js := findJSON{Query: query, Count: len(ranked)}
	js.Window.Start = w.Start.Format(time.RFC3339)
	js.Window.End = w.End.Format(time.RFC3339)
	for _, c := range ranked {
		js.Results = append(js.Results, findResultJSON{
			Path:      c.Path,
			Size:      c.Size,
			ModTime:   c.When().Format(time.RFC3339),
			Ext:       c.Ext,
			ParentDir: c.ParentDir,
			Score:     round3(c.Score),
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(js)
}

func emitFindTable(out io.Writer, w when.Window, ranked []rank.Candidate) error {
	header := fmt.Sprintf("Window: %s \u2192 %s  (%d match%s)",
		render.Date(w.Start), render.Date(w.End),
		len(ranked), plural(len(ranked)))
	if err := render.Line(out, header); err != nil {
		return err
	}
	if len(ranked) == 0 {
		return render.Line(out, "No files in that time range. Try a wider phrase or index more paths.")
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "SCORE\tWHEN\tSIZE\tPATH"); err != nil {
		return err
	}
	for _, c := range ranked {
		if _, err := fmt.Fprintf(tw, "%0.2f\t%s\t%s\t%s\n",
			c.Score,
			render.DateTime(c.When()),
			render.Bytes(c.Size),
			c.Path,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// round3 rounds to three decimal places for stable JSON output.
func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

// labelWindow collapses one or more matched labels into a single window
// spanning the earliest start to the latest end. A session name is not
// guaranteed unique (two trips could share a name), so find searches the union
// of every matching window rather than picking one arbitrarily.
func labelWindow(labels []store.Label) when.Window {
	w := when.Window{Start: labels[0].Start, End: labels[0].End}
	for _, l := range labels[1:] {
		if l.Start.Before(w.Start) {
			w.Start = l.Start
		}
		if l.End.After(w.End) {
			w.End = l.End
		}
	}
	return w
}
