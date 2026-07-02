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

func TestRankInsideWindowScoresOne(t *testing.T) {
	w := march2025()
	cands := []Candidate{
		{Path: "/in", ModTime: ts(2025, 3, 15)},
	}
	got := Rank(cands, w)
	if got[0].Score != 1.0 {
		t.Errorf("in-window score = %v, want 1.0", got[0].Score)
	}
}

func TestRankOrdersByProximity(t *testing.T) {
	w := march2025()
	cands := []Candidate{
		{Path: "/far", ModTime: ts(2025, 7, 1)},  // ~3 months after
		{Path: "/in", ModTime: ts(2025, 3, 10)},  // inside
		{Path: "/near", ModTime: ts(2025, 4, 5)}, // few days after
	}
	got := Rank(cands, w)
	wantOrder := []string{"/in", "/near", "/far"}
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
	// should score as in-window because When() prefers capture time.
	cands := []Candidate{
		{Path: "/photo", ModTime: ts(2025, 8, 1), CaptureTime: ts(2025, 3, 20)},
	}
	got := Rank(cands, w)
	if got[0].Score != 1.0 {
		t.Errorf("capture-time-in-window score = %v, want 1.0", got[0].Score)
	}
}

func TestRankSymmetricDecay(t *testing.T) {
	w := march2025()
	// Equal distance before the start and after the end should score equally.
	before := Candidate{Path: "/before", ModTime: time.Date(2025, 2, 19, 0, 0, 0, 0, time.UTC)} // 10 days before start
	after := Candidate{Path: "/after", ModTime: time.Date(2025, 4, 11, 0, 0, 0, 0, time.UTC)}   // 10 days after end
	got := Rank([]Candidate{before, after}, w)
	if got[0].Score == 0 || got[1].Score == 0 {
		t.Fatalf("expected non-zero decayed scores, got %v", scores(got))
	}
	if diff := got[0].Score - got[1].Score; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("symmetric distances scored differently: %v", scores(got))
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
