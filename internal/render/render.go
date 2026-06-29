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
)

// Line writes a single message followed by a newline to w. It's a trivial
// helper, but routing all command output through render keeps a single seam
// for future styling (color, width detection) without touching call sites.
func Line(w io.Writer, msg string) error {
	_, err := fmt.Fprintln(w, msg)
	return err
}
