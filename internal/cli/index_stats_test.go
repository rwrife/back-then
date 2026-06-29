package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture creates a small tree and returns its root.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "alpha")
	write("b.txt", "bravo")
	write("c.md", "charlie")
	write("node_modules/skip.js", "nope")
	return root
}

func TestIndexThenStats(t *testing.T) {
	root := writeFixture(t)
	db := filepath.Join(t.TempDir(), "index.db")

	out, err := execute(t, "index", root, "--db", db)
	if err != nil {
		t.Fatalf("index returned error: %v\noutput: %s", err, out)
	}
	// node_modules is in the default skip list, so only 3 files are seen.
	if !strings.Contains(out, "3 files seen") {
		t.Fatalf("index summary unexpected: %q", out)
	}
	if !strings.Contains(out, "3 updated") {
		t.Fatalf("index should report 3 updated; got: %q", out)
	}

	// Second run is fully incremental.
	out, err = execute(t, "index", root, "--db", db)
	if err != nil {
		t.Fatalf("second index error: %v", err)
	}
	if !strings.Contains(out, "0 updated") || !strings.Contains(out, "3 unchanged") {
		t.Fatalf("second index should be incremental; got: %q", out)
	}

	// stats (table)
	out, err = execute(t, "stats", "--db", db)
	if err != nil {
		t.Fatalf("stats error: %v", err)
	}
	if !strings.Contains(out, "Files:      3") {
		t.Errorf("stats missing file count; got: %q", out)
	}
	if !strings.Contains(out, ".txt") {
		t.Errorf("stats missing top extension; got: %q", out)
	}
}

func TestStatsJSON(t *testing.T) {
	root := writeFixture(t)
	db := filepath.Join(t.TempDir(), "index.db")
	if _, err := execute(t, "index", root, "--db", db); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "stats", "--db", db, "--json")
	if err != nil {
		t.Fatalf("stats --json error: %v", err)
	}

	var parsed struct {
		Files     int   `json:"files"`
		TotalSize int64 `json:"total_size_bytes"`
		TopExts   []struct {
			Ext   string `json:"ext"`
			Count int    `json:"count"`
		} `json:"top_exts"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("stats --json not valid JSON: %v\n%s", err, out)
	}
	if parsed.Files != 3 {
		t.Errorf("json files = %d, want 3", parsed.Files)
	}
	if len(parsed.TopExts) == 0 || parsed.TopExts[0].Ext != ".txt" {
		t.Errorf("json top_exts unexpected: %+v", parsed.TopExts)
	}
}

func TestStatsEmptyIndex(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	out, err := execute(t, "stats", "--db", db)
	if err != nil {
		t.Fatalf("stats on empty index error: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("expected empty-index message; got: %q", out)
	}
}

func TestIndexRejectsMissingPath(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := execute(t, "index", missing, "--db", db); err == nil {
		t.Error("expected error indexing a missing path, got nil")
	}
}

func TestIndexRejectsNoArgs(t *testing.T) {
	if _, err := execute(t, "index"); err == nil {
		t.Error("expected error when index is given no paths, got nil")
	}
}

func TestRootHelpListsNewCommands(t *testing.T) {
	out, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	for _, want := range []string{"index", "stats"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help should list %q command; got: %q", want, out)
		}
	}
}
