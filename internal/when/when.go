// Package when parses fuzzy human time phrases ("last spring", "around march",
// "the week of jun 3", "2 years ago") into a concrete [start, end] window.
//
// The real parser lands in M3. This stub defines the Window type that find/
// rank build on so the shape is stable.
package when

import "time"

// Window is a half-open time interval [Start, End) that a fuzzy time phrase
// resolves to. back-then ranks files by their proximity to this window.
type Window struct {
	Start time.Time
	End   time.Time
}
