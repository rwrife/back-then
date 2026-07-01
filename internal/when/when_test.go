package when

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// update regenerates the golden file when set: `go test ./internal/when -update`.
var update = flag.Bool("update", false, "update golden files")

// fixedNow is the reference instant all golden cases resolve against. It is a
// Tuesday (2025-05-13) in mid-spring so "this/last spring", "last month", etc.
// have unambiguous expected windows. UTC keeps the golden output stable across
// machines.
var fixedNow = time.Date(2025, time.May, 13, 14, 30, 0, 0, time.UTC)

// goldenPhrases is the ordered list of phrases exercised by the golden test.
// Keep new cases appended so existing golden lines stay stable in diffs.
var goldenPhrases = []string{
	"today",
	"yesterday",
	"tomorrow",
	"this week",
	"last week",
	"this month",
	"last month",
	"this year",
	"last year",
	"3 days ago",
	"1 day ago",
	"2 weeks ago",
	"6 months ago",
	"2 years ago",
	"march",
	"last march",
	"this march",
	"december", // most recent december is the prior year
	"may",      // current month
	"march 2024",
	"dec 2019",
	"spring",
	"last spring", // mid-spring now -> previous spring
	"this spring",
	"summer",
	"winter",
	"fall",
	"autumn",
	"2018",
	"2024-03-15",
	"2024-03",
	"2024/06/01",
	// Filler-word tolerance and casing.
	"around March",
	"  Last   Spring  ",
	"sometime in 2018",
	"in march 2024", // "in" filler dropped -> "march 2024"
}

func TestParseGolden(t *testing.T) {
	var b strings.Builder
	for _, phrase := range goldenPhrases {
		w, err := Parse(phrase, fixedNow)
		if err != nil {
			b.WriteString(line(phrase, "ERROR: "+err.Error()))
			continue
		}
		b.WriteString(line(phrase, fmtWindow(w)))
	}
	got := b.String()

	goldenPath := filepath.Join("testdata", "parse_golden.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	// Normalize CRLF -> LF so the comparison is stable even if the golden
	// file was checked out with Windows line endings (e.g. a stray git
	// autocrlf config). The generated output always uses \n.
	wantStr := strings.ReplaceAll(string(want), "\r\n", "\n")
	if got != wantStr {
		t.Errorf("golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, wantStr)
	}
}

// line formats one phrase/result row for the golden file.
func line(phrase, result string) string {
	return strings.TrimSpace(phrase) + "\t=>\t" + result + "\n"
}

// fmtWindow renders a window as "start | end" in RFC3339 for golden stability.
func fmtWindow(w Window) string {
	return w.Start.Format(time.RFC3339) + " | " + w.End.Format(time.RFC3339)
}

func TestParseEmptyAndUnknown(t *testing.T) {
	for _, s := range []string{"", "   ", "asdf qwerty", "the around of"} {
		if _, err := Parse(s, fixedNow); err == nil {
			t.Errorf("Parse(%q) expected error, got nil", s)
		}
	}
}

func TestParseWindowInvariants(t *testing.T) {
	for _, phrase := range goldenPhrases {
		w, err := Parse(phrase, fixedNow)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", phrase, err)
			continue
		}
		if !w.End.After(w.Start) {
			t.Errorf("Parse(%q): window end %v not after start %v", phrase, w.End, w.Start)
		}
		mid := w.Mid()
		if mid.Before(w.Start) || !mid.Before(w.End) {
			t.Errorf("Parse(%q): mid %v not within [%v,%v)", phrase, mid, w.Start, w.End)
		}
	}
}

func TestSpecificWindows(t *testing.T) {
	cases := []struct {
		phrase string
		start  time.Time
		end    time.Time
	}{
		{"today", day(2025, 5, 13), day(2025, 5, 14)},
		{"yesterday", day(2025, 5, 12), day(2025, 5, 13)},
		{"this month", day(2025, 5, 1), day(2025, 6, 1)},
		{"last month", day(2025, 4, 1), day(2025, 5, 1)},
		{"this year", day(2025, 1, 1), day(2026, 1, 1)},
		{"last year", day(2024, 1, 1), day(2025, 1, 1)},
		{"2 years ago", day(2023, 1, 1), day(2024, 1, 1)},
		{"december", day(2024, 12, 1), day(2025, 1, 1)}, // most recent past december
		{"march 2024", day(2024, 3, 1), day(2024, 4, 1)},
		{"2018", day(2018, 1, 1), day(2019, 1, 1)},
		{"2024-03-15", day(2024, 3, 15), day(2024, 3, 16)},
		// Mid-spring (May) now: "last spring" => spring 2024.
		{"last spring", day(2024, 3, 1), day(2024, 6, 1)},
		{"this spring", day(2025, 3, 1), day(2025, 6, 1)},
	}
	for _, c := range cases {
		w, err := Parse(c.phrase, fixedNow)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.phrase, err)
			continue
		}
		if !w.Start.Equal(c.start) || !w.End.Equal(c.end) {
			t.Errorf("Parse(%q) = [%v, %v), want [%v, %v)",
				c.phrase, w.Start, w.End, c.start, c.end)
		}
	}
}

// day is a UTC midnight helper for expected-window construction.
func day(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}
