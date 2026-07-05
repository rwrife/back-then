package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestForgetDryRunDefault verifies forget previews without deleting unless
// --yes is passed: the entry must still be findable afterward.
func TestForgetDryRunDefault(t *testing.T) {
	now := time.Now()
	db := seedIndex(t, map[string]time.Time{
		"docs/today.txt": now.Add(-2 * time.Hour),
	})

	out, err := executeDB(t, db, "forget", "today")
	if err != nil {
		t.Fatalf("forget today (dry run): %v", err)
	}
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "--yes") {
		t.Errorf("expected dry-run notice mentioning --yes; got: %q", out)
	}

	// The file must still be in the index (dry run deletes nothing).
	find, err := executeDB(t, db, "find", "today")
	if err != nil {
		t.Fatalf("find after dry run: %v", err)
	}
	if !strings.Contains(find, "today.txt") {
		t.Errorf("dry-run forget must not delete; today.txt gone. find: %q", find)
	}
}

// TestForgetYesDeletes verifies --yes actually prunes the matching entries so
// a subsequent find no longer returns them.
func TestForgetYesDeletes(t *testing.T) {
	now := time.Now()
	db := seedIndex(t, map[string]time.Time{
		"docs/today.txt":  now.Add(-2 * time.Hour),
		"old/ancient.txt": now.AddDate(-6, 0, 0),
	})

	out, err := executeDB(t, db, "forget", "today", "--yes")
	if err != nil {
		t.Fatalf("forget today --yes: %v", err)
	}
	if !strings.Contains(out, "Forgot") {
		t.Errorf("expected confirmation of deletion; got: %q", out)
	}

	// today.txt should be gone from find results...
	find, err := executeDB(t, db, "find", "today")
	if err != nil {
		t.Fatalf("find after forget: %v", err)
	}
	if strings.Contains(find, "today.txt") {
		t.Errorf("today.txt should have been forgotten; find: %q", find)
	}

	// ...but the ancient file (outside the window) must survive.
	old, err := executeDB(t, db, "find", "6 years ago")
	if err != nil {
		t.Fatalf("find 6 years ago: %v", err)
	}
	if !strings.Contains(old, "ancient.txt") {
		t.Errorf("out-of-window file wrongly forgotten; find: %q", old)
	}
}

// TestForgetEmptyRangeMessage checks the friendly no-op message when nothing
// falls in the requested window.
func TestForgetEmptyRangeMessage(t *testing.T) {
	db := seedIndex(t, map[string]time.Time{
		"recent.txt": time.Now(),
	})
	out, err := executeDB(t, db, "forget", "1990", "--yes")
	if err != nil {
		t.Fatalf("forget 1990: %v", err)
	}
	if !strings.Contains(out, "Nothing indexed in that range") {
		t.Errorf("expected empty-range message; got: %q", out)
	}
}

// TestForgetJSONShape validates the --json contract, including that a dry run
// reports applied:false and removed:0 while still matching entries.
func TestForgetJSONShape(t *testing.T) {
	now := time.Now()
	db := seedIndex(t, map[string]time.Time{
		"a.txt": now.Add(-1 * time.Hour),
		"b.txt": now.Add(-3 * time.Hour),
	})

	out, err := executeDB(t, db, "forget", "today", "--json")
	if err != nil {
		t.Fatalf("forget today --json: %v", err)
	}
	var parsed struct {
		Query  string `json:"query"`
		Window struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"window"`
		Matched int64 `json:"matched"`
		Applied bool  `json:"applied"`
		Removed int64 `json:"removed"`
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
	if parsed.Matched < 2 {
		t.Errorf("expected >=2 matched in today's window, got %d", parsed.Matched)
	}
	if parsed.Applied {
		t.Errorf("dry-run JSON should report applied:false")
	}
	if parsed.Removed != 0 {
		t.Errorf("dry-run JSON removed = %d, want 0", parsed.Removed)
	}
}

// TestForgetRejectsMultipleArgs mirrors find: exactly one quoted phrase.
func TestForgetRejectsMultipleArgs(t *testing.T) {
	db := seedIndex(t, map[string]time.Time{"x.txt": time.Now()})
	if _, err := executeDB(t, db, "forget", "last", "spring"); err == nil {
		t.Error("expected error: forget takes exactly one quoted phrase")
	}
}
