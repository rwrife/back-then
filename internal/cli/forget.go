package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/when"
)

// forgetJSON is the stable shape emitted by `back-then forget --json`. It
// reports the resolved window, how many indexed files fall inside it, whether
// the run actually deleted (applied) or only previewed (dry run), and the
// number removed.
type forgetJSON struct {
	Query  string `json:"query"`
	Window struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"window"`
	// Matched is how many indexed files fall in the window.
	Matched int64 `json:"matched"`
	// Applied is true when rows were actually deleted; false for a dry run.
	Applied bool `json:"applied"`
	// Removed is the number of index entries deleted (0 on a dry run).
	Removed int64 `json:"removed"`
}

// newForgetCmd returns the `back-then forget "<time phrase>"` subcommand: it
// resolves a fuzzy time phrase into a window and prunes matching entries from
// the INDEX (never from disk) for privacy or to reclaim space.
//
// Because it is destructive, forget is dry-run by default: it prints what would
// be removed and exits without touching the index. Pass --yes to actually
// delete. This "preview first, opt in to apply" flow keeps a stray `forget`
// from silently wiping part of someone's index.
func newForgetCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var asJSON bool
	var confirm bool

	cmd := &cobra.Command{
		Use:   `forget "<time phrase>"`,
		Short: "Prune index entries in a time range (index only, not your files)",
		Long: `Remove indexed entries whose timestamp falls in a fuzzy time window.

forget is for privacy and space: it deletes rows from the local INDEX only and
never touches the files on disk. The window is resolved exactly like find, so
"forget last spring" prunes precisely the entries find would anchor to that
span. Examples:

  back-then forget "2019"            # preview what would be pruned
  back-then forget "last spring"     # preview
  back-then forget "2019" --yes      # actually prune those entries

Because it is destructive, forget only PREVIEWS by default (a dry run). Pass
--yes to apply the deletion. Re-running index on the same paths will, of
course, re-add the files; forget is about the current index, not permanence.

Use --json for scripting.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			now := time.Now()

			w, err := when.Parse(query, now)
			if err != nil {
				return err
			}

			path, err := resolveDBPath(*dbPath, cfg.DB)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}
			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			matched, err := st.CountInWindow(w)
			if err != nil {
				return err
			}

			var removed int64
			if confirm && matched > 0 {
				removed, err = st.Forget(w)
				if err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()
			if asJSON {
				return emitForgetJSON(out, query, w, matched, confirm, removed)
			}
			return emitForgetText(out, w, matched, confirm, removed)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().BoolVar(&confirm, "yes", false, "actually delete (without this, forget only previews)")

	return cmd
}

func emitForgetJSON(out io.Writer, query string, w when.Window, matched int64, applied bool, removed int64) error {
	js := forgetJSON{Query: query, Matched: matched, Applied: applied, Removed: removed}
	js.Window.Start = w.Start.Format(time.RFC3339)
	js.Window.End = w.End.Format(time.RFC3339)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(js)
}

func emitForgetText(out io.Writer, w when.Window, matched int64, applied bool, removed int64) error {
	header := fmt.Sprintf("Window: %s \u2192 %s  (%d entr%s in range)",
		render.Date(w.Start), render.Date(w.End), matched, entryPlural(matched))
	if err := render.Line(out, header); err != nil {
		return err
	}

	switch {
	case matched == 0:
		return render.Line(out, "Nothing indexed in that range; nothing to forget.")
	case applied:
		return render.Line(out, fmt.Sprintf("Forgot %d index entr%s. Files on disk are untouched.",
			removed, entryPlural(removed)))
	default:
		return render.Line(out, fmt.Sprintf(
			"Dry run: %d entr%s would be pruned from the index (files on disk untouched). Re-run with --yes to apply.",
			matched, entryPlural(matched)))
	}
}

// entryPlural returns the plural suffix for "entry"/"entries" given a count.
func entryPlural(n int64) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
