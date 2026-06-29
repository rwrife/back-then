//go:build darwin

package walk

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns the file creation/birth time on macOS, where the BSD
// stat struct exposes a real birthtime (Birthtimespec).
func birthTime(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		bt := st.Birthtimespec
		if bt.Sec == 0 && bt.Nsec == 0 {
			return time.Time{}
		}
		return time.Unix(bt.Sec, bt.Nsec)
	}
	return time.Time{}
}
