//go:build linux

package walk

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns the file creation/birth time when available.
//
// On Linux, true birth time (btime) is only exposed via statx(2), which Go's
// os.FileInfo does not surface. The widely-available Stat_t carries atime,
// mtime, and ctime — but ctime is the inode-change time, not creation, so we
// deliberately do NOT use it as a birth time (it would be misleading). We
// return the zero value; callers fall back to ModTime where a creation time
// is needed. A statx-based path can be added later without changing the
// FileSignal contract.
func birthTime(info fs.FileInfo) time.Time {
	if _, ok := info.Sys().(*syscall.Stat_t); ok {
		// Intentionally no btime on Linux via Stat_t; see doc above.
		return time.Time{}
	}
	return time.Time{}
}
