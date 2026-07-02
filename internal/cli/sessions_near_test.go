package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// burstFixture writes files whose modification times form two clear bursts:
//   - burst 1: three files around a base instant (minutes apart)
//   - burst 2: two files ~8h later
//
// It returns the root and the absolute path of one burst-1 file (useful as a
// `near` target).
func burstFixture(t *testing.T) (root, target string) {
	t.Helper()
	root = t.TempDir()
	base := time.Date(2025, time.March, 10, 9, 0, 0, 0, time.UTC)

	write := func(rel string, offsetMin int) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(offsetMin) * time.Minute)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Burst 1 (folder "trip").
	target = write("trip/photo1.jpg", 0)
	write("trip/photo2.jpg", 3)
	write("trip/photo3.jpg", 6)
	// Burst 2 (folder "work"), ~8h later.
	write("work/report.pdf", 8*60)
	write("work/notes.txt", 8*60+4)
	return root, target
}

func indexBurst(t *testing.T) (db, target string) {
	t.Helper()
	root, tgt := burstFixture(t)
	db = filepath.Join(t.TempDir(), "index.db")
	if out, err := execute(t, "index", root, "--db", db); err != nil {
		t.Fatalf("index error: %v\n%s", err, out)
	}
	return db, tgt
}

func TestSessionsTable(t *testing.T) {
	db, _ := indexBurst(t)

	out, err := execute(t, "sessions", "--db", db)
	if err != nil {
		t.Fatalf("sessions error: %v\n%s", err, out)
	}
	// Two bursts -> expect the header plus the dominant extensions of each.
	if !strings.Contains(out, "WHEN") || !strings.Contains(out, "TOP FOLDER") {
		t.Errorf("sessions table missing header; got: %q", out)
	}
	if !strings.Contains(out, ".jpg") {
		t.Errorf("sessions table missing .jpg burst; got: %q", out)
	}
	if !strings.Contains(out, ".pdf") && !strings.Contains(out, ".txt") {
		t.Errorf("sessions table missing work burst; got: %q", out)
	}
}

func TestSessionsJSON(t *testing.T) {
	db, _ := indexBurst(t)

	out, err := execute(t, "sessions", "--db", db, "--json")
	if err != nil {
		t.Fatalf("sessions --json error: %v", err)
	}
	var arr []struct {
		Start     string `json:"start"`
		End       string `json:"end"`
		Count     int    `json:"count"`
		TopFolder string `json:"top_folder"`
		Exts      []struct {
			Ext   string `json:"ext"`
			Count int    `json:"count"`
		} `json:"dominant_exts"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("sessions --json not valid JSON: %v\n%s", err, out)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %s", len(arr), out)
	}
	// Newest-first: the first entry is the work burst (2 files).
	if arr[0].Count != 2 {
		t.Errorf("first (newest) session count = %d, want 2", arr[0].Count)
	}
	if arr[1].Count != 3 {
		t.Errorf("second session count = %d, want 3", arr[1].Count)
	}
}

func TestSessionsGapFlagMergesBursts(t *testing.T) {
	db, _ := indexBurst(t)

	// With a 12h gap, the two bursts (~8h apart) collapse into one session.
	out, err := execute(t, "sessions", "--db", db, "--json", "--gap", "12h")
	if err != nil {
		t.Fatalf("sessions --gap error: %v", err)
	}
	var arr []struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if len(arr) != 1 || arr[0].Count != 5 {
		t.Errorf("with 12h gap expected 1 session of 5 files; got %+v", arr)
	}
}

func TestSessionsEmptyIndex(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	out, err := execute(t, "sessions", "--db", db)
	if err != nil {
		t.Fatalf("sessions on empty index error: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("expected empty-index message; got: %q", out)
	}
}

func TestNearTable(t *testing.T) {
	db, target := indexBurst(t)

	out, err := execute(t, "near", target, "--db", db)
	if err != nil {
		t.Fatalf("near error: %v\n%s", err, out)
	}
	// The other two burst-1 photos should appear; the target itself must not.
	if !strings.Contains(out, "photo2.jpg") || !strings.Contains(out, "photo3.jpg") {
		t.Errorf("near output missing co-arriving photos; got: %q", out)
	}
	if strings.Contains(out, "photo1.jpg\t") {
		t.Errorf("near output should exclude the target row; got: %q", out)
	}
	// Work-burst files are ~8h away, outside the default 6h window.
	if strings.Contains(out, "report.pdf") {
		t.Errorf("near default window should exclude the 8h-away file; got: %q", out)
	}
}

func TestNearWindowFlag(t *testing.T) {
	db, target := indexBurst(t)

	// Widen to 12h so the work burst is pulled in too.
	out, err := execute(t, "near", target, "--db", db, "--json", "--window", "12h")
	if err != nil {
		t.Fatalf("near --window error: %v", err)
	}
	var nj struct {
		Target  string `json:"target"`
		Results []struct {
			Path    string `json:"path"`
			OffsetS int64  `json:"offset_seconds"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &nj); err != nil {
		t.Fatalf("near --json invalid: %v\n%s", err, out)
	}
	// 2 photos + 2 work files = 4 co-arriving within 12h.
	if len(nj.Results) != 4 {
		t.Errorf("near --window 12h expected 4 results, got %d: %+v", len(nj.Results), nj.Results)
	}
	// Closest first: photo2 (+3m) leads.
	if len(nj.Results) > 0 && !strings.HasSuffix(nj.Results[0].Path, "photo2.jpg") {
		t.Errorf("closest result = %s, want photo2.jpg", nj.Results[0].Path)
	}
}

func TestNearMissingTargetErrors(t *testing.T) {
	db, _ := indexBurst(t)
	missing := filepath.Join(t.TempDir(), "not-indexed.txt")
	if _, err := execute(t, "near", missing, "--db", db); err == nil {
		t.Error("expected error for a target not in the index, got nil")
	}
}

func TestNearRejectsNoArgs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	if _, err := execute(t, "near", "--db", db); err == nil {
		t.Error("expected error when near is given no file, got nil")
	}
}

func TestRootHelpListsSessionsAndNear(t *testing.T) {
	out, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	for _, want := range []string{"sessions", "near"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help should list %q command; got: %q", want, out)
		}
	}
}
