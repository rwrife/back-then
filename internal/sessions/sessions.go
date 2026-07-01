// Package sessions reconstructs "sessions" — clusters of files that arrived or
// changed close together in time (and often in the same folder). Sessions are
// back-then's core episodic primitive: humans remember in episodes, so we let
// them browse time instead of folders.
//
// Implemented in M4 (sessions + near).
//
// The clustering is deliberately simple and dependency-free: files are ordered
// along their effective timeline and split wherever the gap between two
// consecutive files exceeds a threshold. The threshold is folder-aware — files
// in the same directory tolerate a wider gap before a new session begins,
// because a working session in one folder often has natural lulls, whereas a
// jump to an unrelated folder is a stronger "new episode" signal.
package sessions

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rwrife/back-then/internal/store"
)

// DefaultGap is the base inter-file gap that separates two sessions. A gap
// larger than this between consecutive files (after the folder adjustment)
// starts a new session. Two hours comfortably groups a burst of downloads or
// an editing session while separating distinct sittings.
const DefaultGap = 2 * time.Hour

// sameFolderGapFactor widens the gap tolerance when consecutive files share a
// parent directory: staying in one folder across a lull is still "one
// episode," so we allow up to DefaultGap*factor before splitting.
const sameFolderGapFactor = 2

// Session is a time-contiguous cluster of files.
type Session struct {
	// Start and End bracket the session on the timeline (effective times of
	// the first and last file; End >= Start).
	Start time.Time
	End   time.Time
	// Files are the members, in timeline order.
	Files []store.FileRecord
}

// Count is the number of files in the session.
func (s Session) Count() int { return len(s.Files) }

// Duration is the span from the first to the last file.
func (s Session) Duration() time.Duration { return s.End.Sub(s.Start) }

// TopFolder returns the parent directory that holds the most files in the
// session (ties broken by the lexicographically smaller path for stability),
// along with how many of the session's files live there.
func (s Session) TopFolder() (string, int) {
	counts := map[string]int{}
	for _, f := range s.Files {
		counts[f.ParentDir]++
	}
	var best string
	var bestN int
	for dir, n := range counts {
		if n > bestN || (n == bestN && dir < best) {
			best, bestN = dir, n
		}
	}
	return best, bestN
}

// DominantExts returns up to topN extensions in the session, most-frequent
// first (ties broken alphabetically). Empty extensions are reported as
// "(none)" so the summary never shows a blank token.
func (s Session) DominantExts(topN int) []ExtCount {
	if topN <= 0 {
		topN = 3
	}
	counts := map[string]int{}
	for _, f := range s.Files {
		ext := f.Ext
		if ext == "" {
			ext = "(none)"
		}
		counts[ext]++
	}
	out := make([]ExtCount, 0, len(counts))
	for ext, n := range counts {
		out = append(out, ExtCount{Ext: ext, Count: n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Ext < out[j].Ext
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// ExtCount pairs an extension with its occurrence count within a session.
type ExtCount struct {
	Ext   string
	Count int
}

// Options tunes clustering. The zero value is valid and uses DefaultGap.
type Options struct {
	// Gap is the base separation between sessions. When <= 0, DefaultGap is
	// used.
	Gap time.Duration
	// FolderAware, when true (the default via Cluster), widens the gap for
	// consecutive files in the same directory. Set explicitly through Cluster.
	FolderAware bool
}

// Cluster groups files into sessions by splitting the timeline wherever the
// gap between consecutive files exceeds the (folder-adjusted) threshold.
//
// Input order does not matter: files are sorted by effective time (then path)
// first, so callers may pass records in any order. The returned sessions are
// in chronological order.
func Cluster(files []store.FileRecord, opts Options) []Session {
	gap := opts.Gap
	if gap <= 0 {
		gap = DefaultGap
	}

	sorted := make([]store.FileRecord, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, tj := sorted[i].EffectiveTime(), sorted[j].EffectiveTime()
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return sorted[i].Path < sorted[j].Path
	})

	var out []Session
	var cur []store.FileRecord
	for i, f := range sorted {
		if i == 0 {
			cur = []store.FileRecord{f}
			continue
		}
		prev := sorted[i-1]
		delta := f.EffectiveTime().Sub(prev.EffectiveTime())

		threshold := gap
		if opts.FolderAware && sameFolder(prev.ParentDir, f.ParentDir) {
			threshold = time.Duration(float64(gap) * sameFolderGapFactor)
		}

		if delta > threshold {
			out = append(out, newSession(cur))
			cur = []store.FileRecord{f}
			continue
		}
		cur = append(cur, f)
	}
	if len(cur) > 0 {
		out = append(out, newSession(cur))
	}
	return out
}

// sameFolder reports whether two parent directories are the same or one is a
// direct ancestor of the other — nearby folders in the same subtree count as
// "the same working area" for gap tolerance.
func sameFolder(a, b string) bool {
	if a == b {
		return true
	}
	// Treat a parent/child relationship as the same area (e.g. downloading into
	// a folder and then a subfolder). Compare cleaned paths to avoid separator
	// quirks.
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(a, b+sep) || strings.HasPrefix(b, a+sep)
}

// newSession wraps an ordered slice of files into a Session, computing its
// Start/End from the first and last members.
func newSession(files []store.FileRecord) Session {
	s := Session{Files: files}
	if len(files) > 0 {
		s.Start = files[0].EffectiveTime()
		s.End = files[len(files)-1].EffectiveTime()
	}
	return s
}

// NearResult is one co-arriving file plus its time offset from the target.
type NearResult struct {
	File store.FileRecord
	// Delta is the signed offset from the target's effective time (negative =
	// before the target, positive = after).
	Delta time.Duration
}

// Near returns the files whose effective time falls within +/- window of the
// target's effective time, excluding the target itself. Results are ordered by
// absolute proximity (closest first), ties broken by path.
//
// The target is matched by path; if it is not present in files, no results are
// returned (callers should verify the target is indexed first for a clearer
// error).
func Near(files []store.FileRecord, targetPath string, window time.Duration) []NearResult {
	var target store.FileRecord
	found := false
	for _, f := range files {
		if f.Path == targetPath {
			target = f
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	anchor := target.EffectiveTime()
	var out []NearResult
	for _, f := range files {
		if f.Path == targetPath {
			continue
		}
		delta := f.EffectiveTime().Sub(anchor)
		if abs(delta) <= window {
			out = append(out, NearResult{File: f, Delta: delta})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := abs(out[i].Delta), abs(out[j].Delta)
		if ai != aj {
			return ai < aj
		}
		return out[i].File.Path < out[j].File.Path
	})
	return out
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
