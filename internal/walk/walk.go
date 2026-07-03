// Package walk performs the filesystem scan: it walks a directory tree,
// applies ignore rules (default skip list + .backthenignore), and extracts
// per-file signals used by the rest of back-then.
//
// Implemented in M2 (indexer). It defines the core FileSignal record so
// downstream packages (store, rank, sessions) can compile against a stable
// shape, and walks a tree emitting one FileSignal per regular file.
package walk

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwrife/back-then/internal/exif"
)

// FileSignal is the set of on-disk signals back-then collects for a single
// file. It intentionally captures only metadata — never file contents.
type FileSignal struct {
	// Path is the absolute path to the file.
	Path string
	// Size is the file size in bytes.
	Size int64
	// ModTime is the last-modified time (always available).
	ModTime time.Time
	// CreateTime is the creation/birth time when the OS/filesystem exposes it;
	// otherwise the zero value.
	CreateTime time.Time
	// Ext is the lowercased file extension including the leading dot (e.g. ".pdf").
	Ext string
	// ParentDir is the absolute path of the containing directory.
	ParentDir string
	// CaptureTime is the EXIF DateTimeOriginal for images when present
	// (populated in M5 via internal/exif); otherwise the zero value.
	CaptureTime time.Time
}

// DefaultSkipDirs is the set of directory names skipped by default during a
// walk. These are noisy, machine-generated, or VCS/cache trees that almost
// never hold the "where did that file go" target. Names are matched
// case-sensitively against the base name of each directory.
var DefaultSkipDirs = map[string]struct{}{
	".git":             {},
	".hg":              {},
	".svn":             {},
	"node_modules":     {},
	".cache":           {},
	"__pycache__":      {},
	".venv":            {},
	"venv":             {},
	".idea":            {},
	".vscode":          {},
	"vendor":           {},
	".terraform":       {},
	".gradle":          {},
	".next":            {},
	".nuxt":            {},
	"dist":             {},
	"build":            {},
	"target":           {},
	".DS_Store":        {},
	".Trash":           {},
	".trash":           {},
	".mypy_cache":      {},
	".pytest_cache":    {},
	".ruff_cache":      {},
	".tox":             {},
	".bundle":          {},
	".npm":             {},
	".pnpm-store":      {},
	"bower_components": {},
}

// Options tunes a Walk. The zero value is valid and uses the default skip
// list with no extra skips.
type Options struct {
	// ExtraSkipDirs is merged with DefaultSkipDirs (by base name).
	ExtraSkipDirs []string
	// IncludeHidden, when true, descends into dot-directories that are not in
	// the skip list. By default hidden directories are walked (only the skip
	// list prunes), so this currently reserves space for future behavior and
	// does not change defaults.
	IncludeHidden bool
}

// VisitFunc is invoked once per regular file discovered during a walk.
// Returning an error aborts the walk and is propagated by Walk.
type VisitFunc func(FileSignal) error

// Walk traverses each root in turn, invoking visit for every regular file
// found, with directories in the skip list pruned. Roots are resolved to
// absolute paths so emitted FileSignal.Path values are stable and comparable.
//
// Symlinks are not followed (WalkDir does not follow them), and non-regular
// files (devices, sockets, FIFOs) are ignored. Per-entry stat errors are
// skipped rather than aborting the whole walk; only a hard error from visit
// stops traversal.
func Walk(roots []string, opts Options, visit VisitFunc) error {
	skip := make(map[string]struct{}, len(DefaultSkipDirs)+len(opts.ExtraSkipDirs))
	for k := range DefaultSkipDirs {
		skip[k] = struct{}{}
	}
	for _, d := range opts.ExtraSkipDirs {
		skip[d] = struct{}{}
	}

	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if err := walkOne(abs, skip, visit); err != nil {
			return err
		}
	}
	return nil
}

func walkOne(root string, skip map[string]struct{}, visit VisitFunc) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable directory or vanished entry: skip it but keep going.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			// Never prune the root itself even if its base name is in the
			// skip list (the user explicitly pointed us at it).
			if path != root {
				if _, skipped := skip[d.Name()]; skipped {
					return fs.SkipDir
				}
			}
			return nil
		}

		// Only index regular files; skip symlinks, devices, sockets, etc.
		if !d.Type().IsRegular() {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			// File raced away between readdir and stat; skip it.
			return nil
		}

		sig := FileSignal{
			Path:       path,
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			CreateTime: birthTime(info),
			Ext:        strings.ToLower(filepath.Ext(path)),
			ParentDir:  filepath.Dir(path),
		}
		// Best-effort EXIF capture date for images. A missing/unreadable date
		// is normal and simply leaves CaptureTime zero (callers fall back to
		// mod/create time); a hard I/O error is swallowed so one bad file never
		// aborts the walk.
		if exif.HasEXIFExt(path) {
			if ct, ok, err := exif.CaptureTime(path); err == nil && ok {
				sig.CaptureTime = ct
			}
		}
		return visit(sig)
	})
}
