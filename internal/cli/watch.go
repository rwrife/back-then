package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/walk"
)

// defaultWatchInterval is how often `back-then watch` re-scans its roots when
// no --interval is given. It's a best-effort poll, so a relaxed cadence keeps
// the tool light on the filesystem while still picking up changes promptly.
const defaultWatchInterval = 30 * time.Second

// minWatchInterval guards against a runaway busy-loop from a tiny or zero
// --interval. Watching is intentionally polling-based (no fsnotify dependency,
// so it stays a single static binary on every OS); a floor keeps that cheap.
const minWatchInterval = time.Second

// indexer is the slice of *store.Store that watch depends on. Narrowing to an
// interface lets tests drive the loop with a fake that records each pass
// without touching a real SQLite index.
type indexer interface {
	Index(roots []string, opts walk.Options) (store.IndexResult, error)
}

// newWatchCmd returns the `back-then watch <path...>` subcommand. It keeps the
// index fresh by re-scanning the given roots on an interval, reusing the exact
// same incremental walk as `index` (unchanged files are skipped), so each pass
// only touches what actually moved. It runs until interrupted (Ctrl-C).
func newWatchCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var skip []string
	var noIgnoreFile bool
	var interval time.Duration
	var once bool

	cmd := &cobra.Command{
		Use:   "watch <path> [path...]",
		Short: "Keep the index fresh by re-scanning on an interval",
		Long: `Watch one or more directory trees and re-index them periodically so
the local index stays current as files come and go.

Each pass runs the same incremental scan as ` + "`back-then index`" + `: files whose
size and modified time are unchanged since the last pass are skipped, so
steady-state watching is cheap. The same --skip list and .backthenignore
handling apply.

Watching is best-effort and polling-based (it re-walks on a timer rather than
subscribing to OS file events), which keeps back-then a single static binary
on every platform. Use --interval to tune the cadence and --once to run a
single pass and exit (handy for cron or scripts).

Press Ctrl-C to stop.

With no paths, back-then watches the roots listed in your config file (see
` + "`back-then config path`" + `).`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			roots := args
			if len(roots) == 0 {
				roots = cfg.Roots
			}
			if len(roots) == 0 {
				return fmt.Errorf("no paths given and no roots configured; pass a path or set \"roots\" in the config file")
			}

			// Validate roots up front so a typo fails fast, mirroring `index`.
			for _, p := range roots {
				info, err := os.Stat(p)
				if err != nil {
					return fmt.Errorf("cannot watch %q: %w", p, err)
				}
				if !info.IsDir() {
					return fmt.Errorf("cannot watch %q: not a directory", p)
				}
			}

			if interval < minWatchInterval {
				interval = minWatchInterval
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

			// Stop cleanly on Ctrl-C / SIGTERM instead of leaving a partial
			// pass or a dangling database handle.
			ctx, stop := signal.NotifyContext(cmd.Context(), stopSignals()...)
			defer stop()

			opts := walk.Options{ExtraSkipDirs: skip, NoIgnoreFile: noIgnoreFile}
			return runWatch(ctx, st, cmd.OutOrStdout(), path, roots, opts, interval, once)
		},
	}

	cmd.Flags().StringSliceVar(&skip, "skip", nil, "extra directory names to skip (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&noIgnoreFile, "no-ignore-file", false, "do not honor .backthenignore files")
	cmd.Flags().DurationVar(&interval, "interval", defaultWatchInterval, "how often to re-scan (e.g. 10s, 2m); floored at 1s")
	cmd.Flags().BoolVar(&once, "once", false, "run a single pass and exit")

	return cmd
}

// runWatch drives the watch loop. It performs an immediate first pass, then
// re-scans every interval until ctx is cancelled (or after one pass when once
// is set). It's separated from the cobra wiring so tests can exercise the loop
// with a fake indexer and a cancellable context.
func runWatch(
	ctx context.Context,
	idx indexer,
	out io.Writer,
	dbPath string,
	roots []string,
	opts walk.Options,
	interval time.Duration,
	once bool,
) error {
	// First pass runs immediately so the user sees activity without waiting a
	// whole interval.
	if err := watchPass(idx, out, dbPath, roots, opts); err != nil {
		return err
	}
	if once {
		return nil
	}

	fmt.Fprintf(out, "Watching %d path(s); re-scanning every %s. Press Ctrl-C to stop.\n", len(roots), interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "Stopped watching.")
			return nil
		case <-ticker.C:
			if err := watchPass(idx, out, dbPath, roots, opts); err != nil {
				// A transient scan error (e.g. a root briefly unmounted) should
				// not tear down a long-lived watcher; report and keep going.
				fmt.Fprintf(out, "watch: scan error: %v\n", err)
			}
		}
	}
}

// watchPass runs one incremental scan and prints a timestamped one-line
// summary. It reports seen/updated/unchanged so a quiet steady state still
// confirms liveness without being noisy.
func watchPass(
	idx indexer,
	out io.Writer,
	dbPath string,
	roots []string,
	opts walk.Options,
) error {
	res, err := idx.Index(roots, opts)
	if err != nil {
		return err
	}
	ts := time.Now().Format("15:04:05")
	_, err = fmt.Fprintf(out,
		"[%s] %s: %d seen, %d updated, %d unchanged.\n",
		ts, dbPath, res.Seen, res.Upserted, res.Skipped,
	)
	return err
}
