package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/walk"
)

// newIndexCmd returns the `back-then index <path...>` subcommand. It walks one
// or more directory trees and upserts per-file signals into the local SQLite
// index, skipping files whose size and mod time are unchanged (incremental).
func newIndexCmd(dbPath *string) *cobra.Command {
	var skip []string
	var noIgnoreFile bool

	cmd := &cobra.Command{
		Use:   "index <path> [path...]",
		Short: "Scan directories and update the local index",
		Long: `Walk one or more directory trees and record per-file signals
(size, modified time, creation time when available, extension, and parent
folder) into the local SQLite index.

Indexing is incremental: files whose size and modified time are unchanged
since the last run are skipped, so re-indexing a tree is fast. A default skip
list (.git, node_modules, caches, build output, and similar) keeps noise out;
add more with --skip.

A .backthenignore file (gitignore-style patterns) in any indexed directory
prunes matching files and folders in that directory and below. Pass
--no-ignore-file to ignore those files and index everything the skip list
allows.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate roots up front so a typo fails fast with a clear message.
			for _, p := range args {
				info, err := os.Stat(p)
				if err != nil {
					return fmt.Errorf("cannot index %q: %w", p, err)
				}
				if !info.IsDir() {
					return fmt.Errorf("cannot index %q: not a directory", p)
				}
			}

			path, err := defaultDBPath(*dbPath)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}

			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			res, err := st.Index(args, walk.Options{
				ExtraSkipDirs: skip,
				NoIgnoreFile:  noIgnoreFile,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, err = fmt.Fprintf(out,
				"Indexed %s: %d files seen, %d updated, %d unchanged.\n",
				path, res.Seen, res.Upserted, res.Skipped,
			)
			return err
		},
	}

	cmd.Flags().StringSliceVar(&skip, "skip", nil, "extra directory names to skip (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&noIgnoreFile, "no-ignore-file", false, "do not honor .backthenignore files")

	return cmd
}
