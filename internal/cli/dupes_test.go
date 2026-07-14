package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dupeFixture writes a tree containing an exact duplicate pair (same bytes)
// plus a same-size-but-different-content decoy and unique files. It returns the
// db path after indexing.
func dupeIndex(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	base := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)

	write := func(rel string, data []byte, offsetMin int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(offsetMin) * time.Minute)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	same := []byte("the same twenty-3 bytes")  // 23 bytes
	decoy := []byte("different twenty3 bytes") // also 23 bytes
	write("downloads/report.pdf", same, 0)
	write("downloads/report (1).pdf", same, 2) // exact dupe of report.pdf
	write("downloads/decoy.pdf", decoy, 4)     // same size + ext, different bytes
	write("misc/unique.txt", []byte("hello"), 6)

	db := filepath.Join(t.TempDir(), "index.db")
	if out, err := execute(t, "index", root, "--db", db); err != nil {
		t.Fatalf("index error: %v\n%s", err, out)
	}
	return db
}

func TestDupesTableMetadata(t *testing.T) {
	db := dupeIndex(t)

	out, err := execute(t, "dupes", "--db", db)
	if err != nil {
		t.Fatalf("dupes error: %v\n%s", err, out)
	}
	// Metadata-only grouping: the three 23-byte .pdf files cluster together.
	if !strings.Contains(out, "SIZE") || !strings.Contains(out, "WASTED") {
		t.Errorf("dupes table missing header; got: %q", out)
	}
	if !strings.Contains(out, "report.pdf") {
		t.Errorf("expected report.pdf in output; got: %q", out)
	}
	if !strings.Contains(out, "duplicate group") {
		t.Errorf("expected summary line; got: %q", out)
	}
}

func TestDupesVerifyExcludesDecoy(t *testing.T) {
	db := dupeIndex(t)

	out, err := execute(t, "dupes", "--db", db, "--verify", "--json")
	if err != nil {
		t.Fatalf("dupes --verify error: %v\n%s", err, out)
	}

	var payload struct {
		TotalGroups int   `json:"total_groups"`
		TotalWasted int64 `json:"total_wasted_bytes"`
		Verified    bool  `json:"verified"`
		Groups      []struct {
			Hash  string `json:"hash"`
			Keep  string `json:"keep"`
			Dupes []struct {
				Path string `json:"path"`
			} `json:"dupes"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if !payload.Verified {
		t.Errorf("expected verified=true")
	}
	if payload.TotalGroups != 1 {
		t.Fatalf("verify should leave exactly 1 confirmed group, got %d", payload.TotalGroups)
	}
	g := payload.Groups[0]
	if g.Hash == "" {
		t.Errorf("verified group must carry a hash")
	}
	if len(g.Dupes) != 1 {
		t.Fatalf("expected 1 confirmed dupe copy, got %d", len(g.Dupes))
	}
	// The decoy (different content) must not appear as a dupe.
	all := g.Keep + " " + g.Dupes[0].Path
	if strings.Contains(all, "decoy.pdf") {
		t.Errorf("decoy must be excluded by --verify; got: %q", all)
	}
	if payload.TotalWasted != 23 {
		t.Errorf("expected 23 wasted bytes, got %d", payload.TotalWasted)
	}
}

func TestDupesPrint0(t *testing.T) {
	db := dupeIndex(t)

	out, err := execute(t, "dupes", "--db", db, "--verify", "--print0")
	if err != nil {
		t.Fatalf("dupes --print0 error: %v\n%s", err, out)
	}
	// Exactly one redundant copy, NUL-terminated, no table framing.
	if strings.Contains(out, "SIZE") || strings.Contains(out, "duplicate group") {
		t.Errorf("--print0 must not print table framing; got: %q", out)
	}
	if !strings.HasSuffix(out, "\x00") {
		t.Errorf("--print0 output must be NUL-terminated; got: %q", out)
	}
	if strings.Count(out, "\x00") != 1 {
		t.Errorf("expected exactly 1 NUL-separated path, got: %q", out)
	}
}

func TestDupesEmptyIndex(t *testing.T) {
	db := filepath.Join(t.TempDir(), "empty.db")
	// Index an empty dir to create the db.
	empty := t.TempDir()
	if out, err := execute(t, "index", empty, "--db", db); err != nil {
		t.Fatalf("index error: %v\n%s", err, out)
	}
	out, err := execute(t, "dupes", "--db", db)
	if err != nil {
		t.Fatalf("dupes on empty index error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("expected empty-index message; got: %q", out)
	}
}
