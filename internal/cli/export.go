package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/export"
	"github.com/rwrife/back-then/internal/render"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/when"
)

// newExportCmd returns the `back-then export` subcommand. Once find/sessions
// have located an episode, export copies those files out into a dated folder
// or a single .zip — the last mile from "I found it" to "I have it in one
// place." It never moves or modifies originals.
//
// Two selectors are supported:
//
//	back-then export --session 20240115-0930      # a specific session
//	back-then export "last spring"                # a fuzzy time window
//
// Exactly one must be supplied.
func newExportCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var (
		sessionID string
		out       string
		asZip     bool
		flat      bool
		force     bool
		dryRun    bool
		asJSON    bool
		gap       time.Duration
	)

	cmd := &cobra.Command{
		Use:   `export [--session <id> | "<time phrase>"]`,
		Short: "Bundle a session or time window into a dated folder or zip",
		Long: `Copy the files of a session or a fuzzy time window into a dated bundle — a
folder or a single .zip — preserving their relative structure.

Select what to export in one of two ways:

  back-then export --session 20240115-0930
  back-then export "last spring"

The bundle is written under --out (default: the current directory) as
back-then-<label-or-window>/ (or .zip with --zip). Originals are never moved or
changed; export only reads them.

Flags:
  --zip       write a single .zip instead of a folder
  --flat      drop every file in the bundle root instead of preserving paths
  --force     overwrite an existing destination
  --dry-run   print what would be exported (and total bytes) without writing
  --json      emit a machine-readable manifest`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Exactly one selector: --session xor a time-phrase argument.
			hasPhrase := len(args) == 1
			if hasPhrase == (sessionID != "") {
				return fmt.Errorf("provide exactly one of --session <id> or a \"<time phrase>\" argument")
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

			var (
				files []store.FileRecord
				label string
			)
			if sessionID != "" {
				files, label, err = selectSession(st, cfg, gap, sessionID)
			} else {
				files, label, err = selectWindow(st, args[0])
			}
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("nothing to export: no files matched the selection")
			}

			layout := export.LayoutPreserve
			if flat {
				layout = export.LayoutFlat
			}
			dest := out
			if dest == "" {
				dest = "."
			}

			res, err := export.Run(export.Options{
				Files:  files,
				Dest:   dest,
				Name:   "back-then-" + label,
				Layout: layout,
				Zip:    asZip,
				Force:  force,
				DryRun: dryRun,
			})
			if err != nil {
				return err
			}

			return emitExport(cmd, res, asJSON)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "id of the session to export (from `back-then sessions`)")
	cmd.Flags().StringVar(&out, "out", "", "destination directory for the bundle (default: current directory)")
	cmd.Flags().BoolVar(&asZip, "zip", false, "write a single .zip instead of a folder")
	cmd.Flags().BoolVar(&flat, "flat", false, "drop all files in the bundle root instead of preserving relative paths")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing destination folder or zip")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be exported without writing anything")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a machine-readable manifest")
	cmd.Flags().DurationVar(&gap, "gap", effectiveGap(cfg, sessions.DefaultGap), "session gap (must match the value used when listing sessions)")

	return cmd
}

// selectSession resolves a session id to its member files, returning the files
// and a bundle label (the tagged name when present, else the session id).
func selectSession(st *store.Store, cfg config.Config, gap time.Duration, id string) ([]store.FileRecord, string, error) {
	all, err := st.AllFiles()
	if err != nil {
		return nil, "", err
	}
	effGap := effectiveGap(cfg, gap)
	sess := sessions.Cluster(all, sessions.Options{Gap: effGap, FolderAware: true})
	for i := range sess {
		if sess[i].ID() == id {
			label := id
			if labels, lerr := st.Labels(); lerr == nil {
				for _, l := range labels {
					if l.ID == id && l.Label != "" {
						label = slugify(l.Label)
						break
					}
				}
			}
			return sess[i].Files, label, nil
		}
	}
	return nil, "", fmt.Errorf("no session with id %q (run `back-then sessions` to list ids)", id)
}

// selectWindow resolves a fuzzy time phrase (or a session label) into its
// files, using the same interpretation `find` uses, and returns a bundle label
// derived from the resolved window.
func selectWindow(st *store.Store, query string) ([]store.FileRecord, string, error) {
	w, err := when.Parse(query, time.Now())
	if err != nil {
		labels, lerr := st.LabelByName(query)
		if lerr != nil {
			return nil, "", lerr
		}
		if len(labels) == 0 {
			return nil, "", err
		}
		w = labelWindow(labels)
	}

	all, err := st.AllFiles()
	if err != nil {
		return nil, "", err
	}
	var files []store.FileRecord
	for _, f := range all {
		t := f.EffectiveTime()
		if !t.Before(w.Start) && t.Before(w.End) {
			files = append(files, f)
		}
	}
	return files, windowLabel(w), nil
}

// windowLabel makes a filesystem-friendly label from a resolved window. A
// same-day window collapses to the date; otherwise it is start_to_end.
func windowLabel(w when.Window) string {
	if sameDay(w.Start, w.End) || w.Start.Equal(w.End) {
		return w.Start.Format("2006-01-02")
	}
	return w.Start.Format("2006-01-02") + "_to_" + w.End.Format("2006-01-02")
}

// slugify lowercases a label and replaces path-hostile runs with single
// hyphens, keeping bundle names tidy and portable.
func slugify(s string) string {
	var b []rune
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b = append(b, r+('a'-'A'))
			prevDash = false
		default:
			if !prevDash {
				b = append(b, '-')
				prevDash = true
			}
		}
	}
	out := string(b)
	// Trim leading/trailing hyphens.
	for len(out) > 0 && out[0] == '-' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if out == "" {
		return "session"
	}
	return out
}

// emitExport prints the result as JSON or a human summary.
func emitExport(cmd *cobra.Command, res export.Result, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	verb := "Exported"
	if res.DryRun {
		verb = "Would export"
	}
	kind := "folder"
	if res.Zip {
		kind = "zip"
	}
	if err := render.Line(out, fmt.Sprintf("%s %d file%s (%s) as %s %s",
		verb, len(res.Entries), plural1(len(res.Entries)),
		render.Bytes(res.TotalBytes), kind, res.Bundle)); err != nil {
		return err
	}
	if res.DryRun {
		for _, e := range res.Entries {
			if err := render.Line(out, fmt.Sprintf("  %s\t(%s)", e.Dest, render.Bytes(e.Size))); err != nil {
				return err
			}
		}
	}
	return nil
}
