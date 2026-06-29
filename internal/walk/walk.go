// Package walk performs the filesystem scan: it walks a directory tree,
// applies ignore rules (default skip list + .backthenignore), and extracts
// per-file signals used by the rest of back-then.
//
// Implemented in M2 (indexer). This stub defines the core FileSignal record
// so downstream packages (store, rank, sessions) can compile against a stable
// shape.
package walk

import "time"

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
	// (populated in M5); otherwise the zero value.
	CaptureTime time.Time
}
