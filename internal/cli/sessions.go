package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
)

// sessionJSON is the stable shape emitted by `back-then sessions --json`.
type sessionJSON struct {
	Start     string         `json:"start"`
	End       string         `json:"end"`
	Count     int            `json:"count"`
	TopFolder string         `json:"top_folder"`
	Exts      []extCountJSON `json:"dominant_exts"`
}

type extCountJSON struct {
	Ext   string `json:"ext"`
	Count int    `json:"count"`
}

// newSessionsCmd returns the `back-then sessions` subcommand. It reconstructs
// time-clustered sessions from the index and lists each with a one-line
// summary: when it happened, how many files, the dominant extensions, and the
// folder most of the files lived in.
func newSessionsCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var asJSON bool
	var limit int
	var gap time.Duration

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List reconstructed sessions (time-clustered bursts of files)",
		Long: `Reconstruct "sessions" — bursts of files that arrived or changed close
together in time — and list them newest first. Each line summarizes when the
session happened, how many files it holds, its dominant file types, and the
folder most of those files lived in.

Sessions are back-then's episodic primitive: instead of browsing folders, you
browse time. Use ` + "`back-then near <file>`" + ` to see the other files from a
particular session.`,
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

			files, err := st.AllFiles()
			if err != nil {
				return err
			}

			sess := sessions.Cluster(files, sessions.Options{Gap: gap, FolderAware: true})

			// Present newest-first: the most recent episode is usually what you
			// are hunting for.
			reverse(sess)
			if limit > 0 && len(sess) > limit {
				sess = sess[:limit]
			}

			out := cmd.OutOrStdout()

			if asJSON {
				arr := make([]sessionJSON, 0, len(sess))
				for _, s := range sess {
					top, _ := s.TopFolder()
					sj := sessionJSON{
						Start:     s.Start.Format(time.RFC3339),
						End:       s.End.Format(time.RFC3339),
						Count:     s.Count(),
						TopFolder: top,
					}
					for _, e := range s.DominantExts(3) {
						sj.Exts = append(sj.Exts, extCountJSON{Ext: e.Ext, Count: e.Count})
					}
					arr = append(arr, sj)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(arr)
			}

			if len(sess) == 0 {
				if len(files) == 0 {
					return render.Line(out, "Index is empty. Run `back-then index <path>` to populate it.")
				}
				return render.Line(out, "No sessions found.")
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(tw, "WHEN\tFILES\tTYPES\tTOP FOLDER"); err != nil {
				return err
			}
			for _, s := range sess {
				top, _ := s.TopFolder()
				if _, err := fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n",
					sessionWhen(s), s.Count(), extsSummary(s.DominantExts(3)), shortenPath(top)); err != nil {
					return err
				}
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().IntVar(&limit, "limit", effectiveLimit(cfg, 20), "maximum number of sessions to list (0 = all)")
	cmd.Flags().DurationVar(&gap, "gap", effectiveGap(cfg, sessions.DefaultGap), "gap between files that starts a new session (e.g. 90m, 3h)")

	return cmd
}

// sessionWhen renders a session's time span compactly. Same-day sessions show
// the date once with a time range; multi-day sessions show both dates.
func sessionWhen(s sessions.Session) string {
	start, end := s.Start, s.End
	if start.IsZero() {
		return "\u2014"
	}
	if sameDay(start, end) {
		if start.Equal(end) {
			return start.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("%s %s\u2013%s", start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"))
	}
	return fmt.Sprintf("%s \u2192 %s", start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"))
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// extsSummary formats dominant extensions like ".jpg×5 .mov×2".
func extsSummary(exts []sessions.ExtCount) string {
	if len(exts) == 0 {
		return "\u2014"
	}
	parts := make([]string, 0, len(exts))
	for _, e := range exts {
		parts = append(parts, fmt.Sprintf("%s\u00d7%d", e.Ext, e.Count))
	}
	return strings.Join(parts, " ")
}

// shortenPath trims a directory to its last two components so the table stays
// readable, prefixing an ellipsis when it was shortened.
func shortenPath(p string) string {
	if p == "" {
		return "\u2014"
	}
	p = filepath.Clean(p)
	parent, base := filepath.Split(p)
	parent = strings.TrimRight(parent, string(filepath.Separator))
	if parent == "" || parent == "." {
		return p
	}
	gp := filepath.Base(parent)
	return "\u2026" + string(filepath.Separator) + filepath.Join(gp, base)
}

// reverse flips a slice of sessions in place (chronological -> newest-first).
func reverse(s []sessions.Session) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
