package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// executeDB runs the root command with a temp --db prepended, capturing
// combined output. It mirrors execute() but targets an isolated index file so
// find/index tests don't touch the user's real database.
func executeDB(t *testing.T, db string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"--db", db}, args...)
	return execute(t, full...)
}

// seedIndex writes files with controlled mod times into a temp tree and indexes
// them into a fresh db. It returns the db path. Times are set relative to now
// so relative phrases ("this month", "today") resolve deterministically enough
// for assertions that check membership rather than exact ordering.
func seedIndex(t *testing.T, files map[string]time.Time) string {
	t.Helper()
	root := t.TempDir()
	for rel, mt := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if _, err := executeDB(t, db, "index", root); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	return db
}

func TestFindUnknownPhraseErrors(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	if _, err := executeDB(t, db, "find", "blah blah nonsense"); err == nil {
		t.Error("expected error for unparseable phrase, got nil")
	}
}

func TestFindEmptyRangeMessage(t *testing.T) {
	// Index a single recent file, then query a far-past year with no matches.
	db := seedIndex(t, map[string]time.Time{
		"recent.txt": time.Now(),
	})
	out, err := executeDB(t, db, "find", "1990")
	if err != nil {
		t.Fatalf("find 1990: %v", err)
	}
	if !strings.Contains(out, "No files in that time range") {
		t.Errorf("expected empty-range message; got: %q", out)
	}
}

func TestFindTableListsMatch(t *testing.T) {
	now := time.Now()
	db := seedIndex(t, map[string]time.Time{
		"docs/today.txt":  now.Add(-2 * time.Hour),
		"old/ancient.txt": now.AddDate(-5, 0, 0),
	})
	out, err := executeDB(t, db, "find", "this month")
	if err != nil {
		t.Fatalf("find this month: %v", err)
	}
	if !strings.Contains(out, "today.txt") {
		t.Errorf("recent file missing from results; got: %q", out)
	}
	if strings.Contains(out, "ancient.txt") {
		t.Errorf("5-year-old file should not match 'this month'; got: %q", out)
	}
	if !strings.Contains(out, "SCORE") || !strings.Contains(out, "PATH") {
		t.Errorf("table header missing; got: %q", out)
	}
}

func TestFindJSONShape(t *testing.T) {
	now := time.Now()
	db := seedIndex(t, map[string]time.Time{
		"a.txt": now.Add(-1 * time.Hour),
		"b.txt": now.Add(-3 * time.Hour),
	})
	out, err := executeDB(t, db, "find", "today", "--json")
	if err != nil {
		t.Fatalf("find today --json: %v", err)
	}
	var parsed struct {
		Query  string `json:"query"`
		Window struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"window"`
		Count   int `json:"count"`
		Results []struct {
			Path  string  `json:"path"`
			Score float64 `json:"score"`
			Ext   string  `json:"ext"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if parsed.Query != "today" {
		t.Errorf("query = %q, want today", parsed.Query)
	}
	if parsed.Window.Start == "" || parsed.Window.End == "" {
		t.Errorf("window not populated: %+v", parsed.Window)
	}
	if parsed.Count != len(parsed.Results) {
		t.Errorf("count %d != len(results) %d", parsed.Count, len(parsed.Results))
	}
	if parsed.Count < 2 {
		t.Errorf("expected both files in today's results, got %d", parsed.Count)
	}
	for _, r := range parsed.Results {
		if r.Score <= 0 || r.Score > 1 {
			t.Errorf("score %v out of (0,1] for %s", r.Score, r.Path)
		}
	}
}

func TestFindLimit(t *testing.T) {
	now := time.Now()
	files := map[string]time.Time{}
	for i := 0; i < 6; i++ {
		files[fmt.Sprintf("f%d.txt", i)] = now.Add(-time.Duration(i) * time.Hour)
	}
	db := seedIndex(t, files)

	out, err := executeDB(t, db, "find", "this month", "--limit", "2", "--json")
	if err != nil {
		t.Fatalf("find --limit 2: %v", err)
	}
	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Count != 2 {
		t.Errorf("with --limit 2, count = %d, want 2", parsed.Count)
	}
}

func TestFindRejectsMultipleArgs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	if _, err := executeDB(t, db, "find", "last", "spring"); err == nil {
		t.Error("expected error: find takes exactly one quoted phrase")
	}
}
