// Package rank scores candidate files for a query by blending time proximity
// to the target window, signal richness (how much trustworthy metadata we
// have), and folder cohesion (files from the same burst/folder reinforce each
// other).
//
// Time proximity is the primary driver: a file's raw closeness to the query
// window sets a ceiling on its score. Richness and cohesion are secondary
// signals that lift a file toward — but never above — that proximity ceiling,
// so a well-documented photo from a busy shoot edges out a lone, metadata-poor
// file at the same distance, while a far-off file can never leapfrog a close
// one on secondary signals alone. When every candidate shares the same
// richness and cohesion, ordering collapses back to pure time proximity.
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

// Weights for the secondary signals, relative to a proximity baseline of 1.
// They are deliberately small so time proximity dominates: at most they lift a
// file's score by richWeight+cohesionWeight of the gap up to its proximity
// ceiling. Tuned so a perfectly-documented, tightly-clustered file can gain a
// meaningful nudge without ever overtaking a closer-in-time file.
const (
	richWeight     = 0.25
	cohesionWeight = 0.20
)

// Rank scores each candidate against the window and returns them sorted
// best-first. The input slice is sorted in place and returned.
//
// Scoring proceeds in two stages:
//
//  1. Time proximity. A file whose effective timestamp lands inside the window
//     scores 1.0; outside, the score decays on a half-life curve so nearby
//     files stay competitive and far-off files fade smoothly. The half-life
//     scales with the window's own width (a whole-year query tolerates more
//     drift than "yesterday"). This proximity value is the file's ceiling.
//
//  2. Secondary blend. Richness (trustworthy metadata) and cohesion (how much
//     the file's folder clusters near the window) lift the score from a
//     floor up toward that proximity ceiling. With no secondary signal a file
//     keeps a baseline fraction of its proximity; with full signal it reaches
//     the ceiling.
func Rank(candidates []Candidate, w when.Window) []Candidate {
	if len(candidates) == 0 {
		return candidates
	}
	halfLife := proximityHalfLife(w)
	cohesion := folderCohesion(candidates, w, halfLife)

	for i := range candidates {
		p := proximityScore(candidates[i].When(), w, halfLife)
		r := richnessScore(candidates[i])
		c := cohesion[candidates[i].ParentDir]
		candidates[i].Score = blend(p, r, c)
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

// blend combines the proximity ceiling p with secondary signals r (richness)
// and c (cohesion), each in [0,1]. The result stays in [0, p]: secondary
// signals interpolate between a baseline share of p and the full ceiling, so
// they can reorder files at similar distances without ever letting a far file
// outscore a near one.
//
// Concretely, secondary = (richWeight*r + cohesionWeight*c) / (richWeight +
// cohesionWeight) is the normalized secondary strength in [0,1], and the
// returned score is p scaled by a factor that runs from baseline (at
// secondary=0) up to 1 (at secondary=1).
func blend(p, r, c float64) float64 {
	const totalW = richWeight + cohesionWeight
	// baseline is the fraction of the proximity ceiling a file keeps with zero
	// secondary signal. Deriving it from the weights keeps a single knob: the
	// stronger the secondary weights, the more a signal-poor file is held back.
	baseline := 1.0 / (1.0 + totalW)
	secondary := (richWeight*r + cohesionWeight*c) / totalW
	factor := baseline + (1.0-baseline)*secondary
	return p * factor
}

// richnessScore rates how much trustworthy metadata a candidate carries, in
// [0,1]. Each independent signal contributes a share:
//   - an EXIF capture time (the strongest "we know when this really happened"
//     signal) is worth half,
//   - a known file extension a quarter,
//   - a plausible non-empty size the remaining quarter.
//
// A photo with real EXIF and a normal size scores high; a zero-byte,
// extensionless scratch file scores low and is nudged down among ties.
func richnessScore(c Candidate) float64 {
	var s float64
	if !c.CaptureTime.IsZero() {
		s += 0.5
	}
	if c.Ext != "" {
		s += 0.25
	}
	if c.Size > 0 {
		s += 0.25
	}
	return s
}

// folderCohesion measures, per parent directory, how much that folder clusters
// around the query window — a proxy for "this file belongs to an event/burst
// rather than sitting alone". For each candidate it accumulates that file's own
// proximity into its folder's bucket, then normalizes every folder's total by
// the strongest folder so the busiest, closest cluster scores 1.0 and lonely
// or far-flung folders score near 0.
//
// Using proximity-weighted counts (not raw counts) means a folder full of
// near-window files reinforces strongly, while a folder that merely has many
// far-off files does not masquerade as a tight cluster.
func folderCohesion(candidates []Candidate, w when.Window, halfLife time.Duration) map[string]float64 {
	weight := make(map[string]float64, len(candidates))
	var max float64
	for i := range candidates {
		p := proximityScore(candidates[i].When(), w, halfLife)
		weight[candidates[i].ParentDir] += p
		if weight[candidates[i].ParentDir] > max {
			max = weight[candidates[i].ParentDir]
		}
	}
	if max <= 0 {
		return weight
	}
	for dir := range weight {
		weight[dir] /= max
	}
	return weight
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
