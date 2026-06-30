// Package rank scores candidate files for a query by blending time proximity
// to the target window, signal richness (how much metadata we have), and
// folder cohesion (files from the same burst/folder reinforce each other).
//
// M3 implements the time-proximity core: candidates are scored purely by how
// close their effective timestamp sits to the query window. The Candidate
// shape and Score field leave room for the richer signals added in M5 without
// reshaping callers.
package rank

import (
	"math"
	"sort"
	"time"

	"github.com/rwrife/back-then/internal/when"
)

// Candidate is one file under consideration for a find query, carrying just
// the fields ranking and rendering need. It is populated by the store from the
// index.
type Candidate struct {
	// Path is the absolute file path.
	Path string
	// Size is the file size in bytes.
	Size int64
	// ModTime is the file's last-modified time.
	ModTime time.Time
	// CaptureTime is the EXIF capture time when known (M5); otherwise zero.
	CaptureTime time.Time
	// Ext is the lowercased extension including the dot.
	Ext string
	// ParentDir is the absolute containing directory.
	ParentDir string

	// Score is filled in by Rank: higher is a better match. It is in [0,1].
	Score float64
}

// When returns the timestamp ranking should measure against: the EXIF capture
// time when present (it best reflects when a photo's moment happened),
// otherwise the modified time.
func (c Candidate) When() time.Time {
	if !c.CaptureTime.IsZero() {
		return c.CaptureTime
	}
	return c.ModTime
}

// Rank scores each candidate against the window by time proximity and returns
// them sorted best-first. The input slice is sorted in place and returned.
//
// Scoring: a file whose timestamp lands inside the window scores 1.0. Outside
// the window the score decays with distance on a half-life curve, so nearby
// files stay competitive and far-off files fade smoothly rather than dropping
// to zero at a hard edge. The half-life scales with the window's own width
// (a broad query like a whole year tolerates more drift than "yesterday").
func Rank(candidates []Candidate, w when.Window) []Candidate {
	halfLife := proximityHalfLife(w)
	for i := range candidates {
		candidates[i].Score = proximityScore(candidates[i].When(), w, halfLife)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		// Tie-break: more recent first, then path for determinism.
		if !candidates[i].When().Equal(candidates[j].When()) {
			return candidates[i].When().After(candidates[j].When())
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

// proximityHalfLife is the distance from the window edge at which the score
// halves. It is a fraction of the window width, clamped to a sane floor so even
// a single-day window keeps some tolerance.
func proximityHalfLife(w when.Window) time.Duration {
	width := w.End.Sub(w.Start)
	hl := width / 2
	const floor = 12 * time.Hour
	if hl < floor {
		hl = floor
	}
	return hl
}

// proximityScore returns a value in [0,1] for how close t is to window w.
// Inside the window: 1.0. Outside: 0.5 ^ (distance / halfLife).
func proximityScore(t time.Time, w when.Window, halfLife time.Duration) float64 {
	if w.Contains(t) {
		return 1.0
	}
	var dist time.Duration
	if t.Before(w.Start) {
		dist = w.Start.Sub(t)
	} else {
		// End is exclusive; measure from the last instant of the window.
		dist = t.Sub(w.End)
	}
	if halfLife <= 0 {
		return 0
	}
	// 0.5 ^ (dist / halfLife) == 2 ^ (-(dist / halfLife)).
	return math.Exp2(-float64(dist) / float64(halfLife))
}
