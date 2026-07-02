// Package render formats back-then results for output: a clean aligned table
// for humans and a --json mode for scripting. Keeping output here means the
// command layer stays free of formatting concerns.
//
// The table/JSON renderers for find/sessions land alongside those commands
// (M2+). This file provides a small shared helper used by every command.
package render

import (
	"fmt"
	"io"
	"time"
)

// Line writes a single message followed by a newline to w. It's a trivial
// helper, but routing all command output through render keeps a single seam
// for future styling (color, width detection) without touching call sites.
func Line(w io.Writer, msg string) error {
	_, err := fmt.Fprintln(w, msg)
	return err
}

// Bytes formats a byte count as a short human-readable string using binary
// (1024-based) units. Example: 1536 -> "1.5 KiB".
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Date formats a timestamp as a compact local date string, or "—" for the
// zero time (unknown).
func Date(t time.Time) string {
	if t.IsZero() {
		return "\u2014"
	}
	return t.Format("2006-01-02")
}

// DateTime formats a timestamp as a compact local date-and-time string, or
// "—" for the zero time. Used where the time of day helps distinguish files
// that share a day (e.g. find results).
func DateTime(t time.Time) string {
	if t.IsZero() {
		return "\u2014"
	}
	return t.Format("2006-01-02 15:04")
}
