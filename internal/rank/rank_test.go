package rank

import (
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/when"
)

func ts(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
}

// march2025 is a one-month window used across cases.
func march2025() when.Window {
	return when.Window{
		Start: time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, time.April, 1, 0, 0, 0, 0, time.UTC),
	}
}

// rich returns a candidate carrying full metadata (size, ext, and — via the
// caller — optionally a capture time) so that its richness signal is maxed out.
// Tests that want to isolate proximity use fully-signaled, cohesive candidates
// so the secondary blend is neutral and scores reflect proximity directly.
func rich(path string, mod time.Time) Candidate {
	return Candidate{Path: path, ModTime: mod, Size: 1024, Ext: ".jpg"}
}

func TestRankInsideWindowFullSignalScoresOne(t *testing.T) {
	w := march2025()
	// A fully-signaled, in-window file (real capture time, known ext, real
	// size, and the only folder so cohesion is maximal) hits the ceiling of
	// 1.0. This preserves the "in-window is a perfect match" contract.
	c := rich("/in", ts(2025, 3, 15))
	c.CaptureTime = ts(2025, 3, 15)
	got := Rank([]Candidate{c}, w)
	if got[0].Score != 1.0 {
		t.Errorf("in-window full-signal score = %v, want 1.0", got[0].Score)
	}
}

func TestRankOrdersByProximity(t *testing.T) {
	w := march2025()
	// Signal-equal candidates (same richness, distinct folders) isolate the
	// proximity ordering: closest to the window ranks first.
	cands := []Candidate{
		rich("/a/far", ts(2025, 7, 1)),  // ~3 months after
		rich("/b/in", ts(2025, 3, 10)),  // inside
		rich("/c/near", ts(2025, 4, 5)), // few days after
	}
	got := Rank(cands, w)
	wantOrder := []string{"/b/in", "/c/near", "/a/far"}
	for i, p := range wantOrder {
		if got[i].Path != p {
			t.Fatalf("rank[%d] = %s, want %s (full: %v)", i, got[i].Path, p, paths(got))
		}
	}
	// Scores must be monotonically non-increasing.
	for i := 1; i < len(got); i++ {
		if got[i].Score > got[i-1].Score {
			t.Errorf("scores not sorted: %v", scores(got))
		}
	}
}

func TestRankCaptureTimePreferred(t *testing.T) {
	w := march2025()
	// ModTime is far (Aug), but CaptureTime sits inside the window: the file
	// should score at the ceiling because When() prefers capture time and the
	// file is otherwise fully signaled and alone in its folder.
	c := rich("/photo", ts(2025, 8, 1))
	c.CaptureTime = ts(2025, 3, 20)
	got := Rank([]Candidate{c}, w)
	if got[0].Score != 1.0 {
		t.Errorf("capture-time-in-window score = %v, want 1.0", got[0].Score)
	}
}

func TestRankSymmetricDecay(t *testing.T) {
	w := march2025()
	// Equal distance before the start and after the end should score equally
	// when the files are otherwise identical in signal.
	before := rich("/x/before", time.Date(2025, 2, 19, 0, 0, 0, 0, time.UTC)) // 10 days before start
	after := rich("/y/after", time.Date(2025, 4, 11, 0, 0, 0, 0, time.UTC))   // 10 days after end
	got := Rank([]Candidate{before, after}, w)
	if got[0].Score == 0 || got[1].Score == 0 {
		t.Fatalf("expected non-zero decayed scores, got %v", scores(got))
	}
	if diff := got[0].Score - got[1].Score; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("symmetric distances scored differently: %v", scores(got))
	}
}

// TestRankRichnessBreaksTies: two files at the same distance (both in-window,
// same folder so cohesion is equal) must be ordered by richness — the one with
// an EXIF capture time and real size/ext outranks the metadata-poor one.
func TestRankRichnessBreaksTies(t *testing.T) {
	w := march2025()
	poor := Candidate{Path: "/dir/poor", ModTime: ts(2025, 3, 15)} // no ext, no size, no capture
	full := Candidate{Path: "/dir/full", ModTime: ts(2025, 3, 15), Size: 2048, Ext: ".jpg", CaptureTime: ts(2025, 3, 15)}
	got := Rank([]Candidate{poor, full}, w)
	if got[0].Path != "/dir/full" {
		t.Fatalf("richer file should rank first, got order %v", paths(got))
	}
	if !(got[0].Score > got[1].Score) {
		t.Errorf("richer file should score strictly higher: %v", scores(got))
	}
	// Both share the same proximity ceiling (1.0); the poor file must sit
	// strictly below it, the full file exactly at it.
	if got[0].Score != 1.0 {
		t.Errorf("full-signal in-window score = %v, want 1.0", got[0].Score)
	}
	if !(got[1].Score < 1.0) {
		t.Errorf("signal-poor in-window score = %v, want < 1.0", got[1].Score)
	}
}

// TestRankFolderCohesionBreaksTies: hold proximity and richness fixed, then
// vary only folder clustering. A file whose folder contains a tight burst of
// near-window files should outrank an identically-scored file that sits alone
// in its own folder.
func TestRankFolderCohesionBreaksTies(t *testing.T) {
	w := march2025()
	// /burst has three in-window files; /lone has one. All are metadata-poor
	// and identical otherwise, so only cohesion differs. Compare the target
	// file in /burst against the lone file (same When, same richness).
	// Cohesion keys on ParentDir (populated by the store), so set it explicitly.
	target := Candidate{Path: "/burst/a", ParentDir: "/burst", ModTime: ts(2025, 3, 15)}
	cands := []Candidate{
		target,
		{Path: "/burst/b", ParentDir: "/burst", ModTime: ts(2025, 3, 16)},
		{Path: "/burst/c", ParentDir: "/burst", ModTime: ts(2025, 3, 17)},
		{Path: "/lone/z", ParentDir: "/lone", ModTime: ts(2025, 3, 15)},
	}
	got := Rank(cands, w)

	var targetScore, loneScore float64
	for _, c := range got {
		switch c.Path {
		case "/burst/a":
			targetScore = c.Score
		case "/lone/z":
			loneScore = c.Score
		}
	}
	if !(targetScore > loneScore) {
		t.Errorf("file in a cohesive burst (%.4f) should outrank a lone file (%.4f)", targetScore, loneScore)
	}
}

// TestRankScoresStayBounded: every score must land in [0,1] and never exceed
// its own proximity ceiling — a far-off file cannot leapfrost a close one on
// secondary signals alone.
func TestRankScoresStayBounded(t *testing.T) {
	w := march2025()
	cands := []Candidate{
		// far but maxed on secondary signals
		{Path: "/rich/far", ModTime: ts(2025, 9, 1), Size: 9999, Ext: ".jpg", CaptureTime: ts(2025, 9, 1)},
		{Path: "/rich/far2", ModTime: ts(2025, 9, 2), Size: 9999, Ext: ".jpg", CaptureTime: ts(2025, 9, 2)},
		// close but metadata-poor and alone
		{Path: "/poor/near", ModTime: ts(2025, 3, 15)},
	}
	got := Rank(cands, w)
	for _, c := range got {
		if c.Score < 0 || c.Score > 1 {
			t.Errorf("score out of [0,1]: %s = %v", c.Path, c.Score)
		}
	}
	// The near-but-poor file must still beat the far-but-rich files.
	if got[0].Path != "/poor/near" {
		t.Errorf("closest file should rank first despite weaker signals, got %v", paths(got))
	}
}

func TestRankEmpty(t *testing.T) {
	if got := Rank(nil, march2025()); got != nil {
		t.Errorf("Rank(nil) = %v, want nil", got)
	}
}

func paths(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path
	}
	return out
}

func scores(cs []Candidate) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = c.Score
	}
	return out
}
