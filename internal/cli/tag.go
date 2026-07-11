package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
)

// newTagCmd returns the `back-then tag <session-id> "<label>"` subcommand. It
// names a reconstructed session so future recall is by episode, not timestamp:
// once a burst is labeled "Berlin trip", `back-then find "Berlin trip"` jumps
// straight to that window.
//
// Session ids come from `back-then sessions` (the ID column). The label and the
// session's resolved window are persisted in the index so find can match the
// name back to a concrete span without re-clustering.
func newTagCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var gap = sessions.DefaultGap

	cmd := &cobra.Command{
		Use:   `tag <session-id> "<label>"`,
		Short: "Give a session a memorable name",
		Long: `Attach a human name to a reconstructed session so you can recall it by
episode instead of by date.

Find a session's id in the ID column of ` + "`back-then sessions`" + `, then:

  back-then tag 20240115-0930 "Berlin trip"

Afterwards the label shows up in ` + "`back-then sessions`" + ` and
` + "`back-then find \"Berlin trip\"`" + ` matches the labeled window. Tagging the
same session again replaces its label. Labels live in the local index; nothing
leaves the machine.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			label := args[1]

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

			effGap := effectiveGap(cfg, gap)
			sess := sessions.Cluster(files, sessions.Options{Gap: effGap, FolderAware: true})

			var target *sessions.Session
			for i := range sess {
				if sess[i].ID() == sessionID {
					target = &sess[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("no session with id %q (run `back-then sessions` to list ids)", sessionID)
			}

			if err := st.AddLabel(target.ID(), label, target.Start, target.End); err != nil {
				return err
			}

			return render.Line(cmd.OutOrStdout(),
				fmt.Sprintf("Tagged session %s (%d file%s) as %q.",
					target.ID(), target.Count(), plural1(target.Count()), label))
		},
	}

	cmd.Flags().DurationVar(&gap, "gap", effectiveGap(cfg, sessions.DefaultGap),
		"gap between files that starts a new session (must match the value used when listing)")

	return cmd
}

// plural1 returns "s" unless n == 1, for pluralizing simple count nouns.
func plural1(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
