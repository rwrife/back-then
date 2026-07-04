// Package walk performs the filesystem scan: it walks a directory tree,
// applies ignore rules (default skip list + .backthenignore), and extracts
// per-file signals used by the rest of back-then.
//
// Implemented in M2 (indexer). It defines the core FileSignal record so
// downstream packages (store, rank, sessions) can compile against a stable
// shape, and walks a tree emitting one FileSignal per regular file.
package walk

import (
	"os"
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
// list with no extra skips and .backthenignore files honored.
type Options struct {
	// ExtraSkipDirs is merged with DefaultSkipDirs (by base name).
	ExtraSkipDirs []string
	// IncludeHidden, when true, descends into dot-directories that are not in
	// the skip list. By default hidden directories are walked (only the skip
	// list prunes), so this currently reserves space for future behavior and
	// does not change defaults.
	IncludeHidden bool
	// NoIgnoreFile disables .backthenignore handling when true. By default
	// (false) an ignore file found in any directory prunes matching entries in
	// that directory and below, gitignore-style.
	NoIgnoreFile bool
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
		if err := walkOne(abs, skip, opts, visit); err != nil {
			return err
		}
	}
	return nil
}

// walkOne traverses a single resolved root. It descends recursively (rather
// than via filepath.WalkDir) so per-directory .backthenignore files can be
// pushed onto an ignore stack on the way down and their scope naturally ends
// when the recursion unwinds. The root directory itself is never pruned by the
// skip list, since the user pointed us at it explicitly.
func walkOne(root string, skip map[string]struct{}, opts Options, visit VisitFunc) error {
	var stack ignoreStack
	if !opts.NoIgnoreFile {
		if sc, ok := loadIgnoreScope(root); ok {
			stack = ignoreStack{sc}
		}
	}
	return walkDir(root, skip, opts, stack, visit)
}

// walkDir visits every regular file in dir and recurses into subdirectories,
// applying the skip list and the current ignore stack. stack already includes
// any ignore file present in dir's ancestors and in dir itself.
func walkDir(dir string, skip map[string]struct{}, opts Options, stack ignoreStack, visit VisitFunc) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Unreadable directory (permissions, vanished): skip it but keep going.
		return nil
	}

	for _, d := range entries {
		path := filepath.Join(dir, d.Name())

		if d.IsDir() {
			if _, skipped := skip[d.Name()]; skipped {
				continue
			}
			if !opts.NoIgnoreFile && stack.ignored(path, true) {
				continue
			}
			// Descend: load this directory's own ignore file (if any) onto a
			// copy of the stack so siblings do not inherit it.
			child := stack
			if !opts.NoIgnoreFile {
				if sc, ok := loadIgnoreScope(path); ok {
					child = append(append(ignoreStack{}, stack...), sc)
				}
			}
			if err := walkDir(path, skip, opts, child, visit); err != nil {
				return err
			}
			continue
		}

		// Only index regular files; skip symlinks, devices, sockets, etc.
		if !d.Type().IsRegular() {
			continue
		}
		if !opts.NoIgnoreFile && stack.ignored(path, false) {
			continue
		}

		info, ierr := d.Info()
		if ierr != nil {
			// File raced away between readdir and stat; skip it.
			continue
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
		if err := visit(sig); err != nil {
			return err
		}
	}
	return nil
}
