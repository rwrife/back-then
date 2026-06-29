//go:build !linux && !darwin && !windows

package walk

import (
	"io/fs"
	"time"
)

// birthTime returns the zero value on platforms where back-then has no
// portable way to read a creation time. Callers fall back to ModTime.
func birthTime(info fs.FileInfo) time.Time {
	_ = info
	return time.Time{}
}
