//go:build windows

package walk

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns the file creation time on Windows, which the Win32 file
// attribute data exposes directly via CreationTime.
func birthTime(info fs.FileInfo) time.Time {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return time.Time{}
}
