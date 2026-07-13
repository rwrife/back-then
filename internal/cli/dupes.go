package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/dupes"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
)

// dupesJSON is the stable shape emitted by `back-then dupes --json`.
type dupesJSON struct {
	TotalGroups int             `json:"total_groups"`
	TotalWasted int64           `json:"total_wasted_bytes"`
	Verified    bool            `json:"verified"`
	Groups      []dupeGroupJSON `json:"groups"`
}

type dupeGroupJSON struct {
	Size   int64          `json:"size_bytes"`
	Ext    string         `json:"ext"`
	Hash   string         `json:"hash,omitempty"`
	Wasted int64          `json:"wasted_bytes"`
	Keep   string         `json:"keep"`
	Dupes  []dupeFileJSON `json:"dupes"`
}

type dupeFileJSON struct {
	Path string `json:"path"`
	When string `json:"when"`
}

// newDupesCmd returns the `back-then dupes` subcommand. It surfaces likely
// duplicate files from the existing index by reusing the session/time-cluster
// primitive: same size + extension arriving close together are very probably
// the same bytes. It never deletes — it reports groups (and, with --print0,
// emits the redundant copies for the user's own `xargs rm`).
func newDupesCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var asJSON bool
	var verify bool
	var print0 bool
	var sessionID string
	var within time.Duration
	var minSize int64

	cmd := &cobra.Command{
		Use:   "dupes",
		Short: "Surface likely-duplicate files from the index",
		Long: `Find likely duplicate files without a full-disk hash sweep. Within the index
(or a scoped session/window), files that share an exact size and extension and
arrived close together are grouped as suspected duplicates — the pattern you get
when you download or copy the same thing twice.

Grouping is metadata-only and cheap. Pass --verify to confirm each group with a
streamed content hash, so reported duplicates are exact. This command is
read-only: it never deletes. Use --print0 to emit the redundant copies (one per
line, NUL-separated) for piping to your own ` + "`xargs -0 rm`" + `.`,
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

			// Scope: a specific session takes precedence over an ad-hoc window.
			if sessionID != "" {
				scoped, ok := filesForSession(files, sessionID, effectiveGap(cfg, sessions.DefaultGap))
				if !ok {
					return fmt.Errorf("no session with id %q; run `back-then sessions` to list ids", sessionID)
				}
				files = scoped
			}

			opts := dupes.Options{MinSize: minSize}
			// An ad-hoc --within window caps how far apart same-size files may
			// be and still count as one candidate cluster.
			if within > 0 {
				opts.Gap = within.Nanoseconds()
			}

			groups := dupes.Find(files, opts)
			if verify {
				groups, err = dupes.Verify(groups)
				if err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()

			// --print0: emit only the redundant copies, NUL-separated, for
			// scripting. Mutually the machine path; ignore table/json framing.
			if print0 {
				for _, g := range groups {
					for _, d := range g.Dupes() {
						if _, err := fmt.Fprintf(out, "%s\x00", d.Path); err != nil {
							return err
						}
					}
				}
				return nil
			}

			if asJSON {
				payload := dupesJSON{
					TotalGroups: len(groups),
					TotalWasted: dupes.TotalWasted(groups),
					Verified:    verify,
					Groups:      make([]dupeGroupJSON, 0, len(groups)),
				}
				for _, g := range groups {
					gj := dupeGroupJSON{
						Size:   g.Size,
						Ext:    g.Ext,
						Hash:   g.Hash,
						Wasted: g.Wasted(),
						Keep:   g.Files[0].Path,
					}
					for _, d := range g.Dupes() {
						gj.Dupes = append(gj.Dupes, dupeFileJSON{
							Path: d.Path,
							When: d.EffectiveTime().Format(time.RFC3339),
						})
					}
					payload.Groups = append(payload.Groups, gj)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}

			if len(groups) == 0 {
				if len(files) == 0 {
					return render.Line(out, "Index is empty. Run `back-then index <path>` to populate it.")
				}
				return render.Line(out, "No duplicates found.")
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(tw, "SIZE\tCOPIES\tWASTED\tKEEP / DUPES"); err != nil {
				return err
			}
			for _, g := range groups {
				role := "keep"
				if _, err := fmt.Fprintf(tw, "%s\t%d\t%s\t%s  %s\n",
					render.Bytes(g.Size), g.Count(), render.Bytes(g.Wasted()), role, g.Files[0].Path); err != nil {
					return err
				}
				for _, d := range g.Dupes() {
					if _, err := fmt.Fprintf(tw, "\t\t\tdupe  %s\n", d.Path); err != nil {
						return err
					}
				}
			}
			if err := tw.Flush(); err != nil {
				return err
			}

			verb := "suspected"
			if verify {
				verb = "confirmed"
			}
			return render.Line(out, fmt.Sprintf("\n%d %s duplicate group(s), %s reclaimable.",
				len(groups), verb, render.Bytes(dupes.TotalWasted(groups))))
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().BoolVar(&verify, "verify", false, "confirm each group by hashing file contents (exact, but reads the files)")
	cmd.Flags().BoolVar(&print0, "print0", false, "print only the redundant copies, NUL-separated, for `xargs -0 rm`")
	cmd.Flags().StringVar(&sessionID, "session", "", "scope to a single session id (see `back-then sessions`)")
	cmd.Flags().DurationVar(&within, "within", 0, "cap how far apart same-size files may be to count as duplicates (e.g. 24h; 0 = no limit)")
	cmd.Flags().Int64Var(&minSize, "min-size", 0, "ignore files smaller than this many bytes")

	return cmd
}

// filesForSession re-clusters the index and returns the member files of the
// session whose ID matches id, plus whether such a session was found.
func filesForSession(files []store.FileRecord, id string, gap time.Duration) ([]store.FileRecord, bool) {
	sess := sessions.Cluster(files, sessions.Options{Gap: gap, FolderAware: true})
	for _, s := range sess {
		if s.ID() == id {
			return s.Files, true
		}
	}
	return nil, false
}
