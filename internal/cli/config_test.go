package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfig writes a config file and points BACK_THEN_CONFIG at it for the
// duration of the test, so the CLI loads exactly these settings. t.Setenv
// restores the previous value automatically.
func withConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BACK_THEN_CONFIG", p)
	return p
}

func TestConfigPathHonorsEnv(t *testing.T) {
	p := withConfig(t, `{}`)
	out, err := execute(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if !strings.Contains(out, p) {
		t.Errorf("config path should print %q; got: %q", p, out)
	}
	if !strings.Contains(out, "present") {
		t.Errorf("config path should mark an existing file present; got: %q", out)
	}
}

func TestConfigPathReportsMissing(t *testing.T) {
	// Point at a path that does not exist.
	missing := filepath.Join(t.TempDir(), "absent.json")
	t.Setenv("BACK_THEN_CONFIG", missing)
	out, err := execute(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if !strings.Contains(out, "not present") {
		t.Errorf("config path should note a missing file; got: %q", out)
	}
}

func TestConfigShowReflectsFile(t *testing.T) {
	withConfig(t, `{"db":"/tmp/x/index.db","roots":["/a","/b"],"gap":"3h","limit":7}`)
	out, err := execute(t, "config", "show", "--json")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	var got struct {
		DB    string   `json:"db"`
		Roots []string `json:"roots"`
		Gap   string   `json:"gap"`
		Limit int      `json:"limit"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse config show json: %v (out=%q)", err, out)
	}
	if got.DB != "/tmp/x/index.db" {
		t.Errorf("db = %q, want /tmp/x/index.db", got.DB)
	}
	if got.Gap != "3h0m0s" {
		t.Errorf("gap = %q, want 3h0m0s", got.Gap)
	}
	if got.Limit != 7 {
		t.Errorf("limit = %d, want 7", got.Limit)
	}
	if len(got.Roots) != 2 {
		t.Errorf("roots = %v, want 2 entries", got.Roots)
	}
}

func TestConfigShowFallsBackToDefaults(t *testing.T) {
	withConfig(t, `{}`)
	out, err := execute(t, "config", "show")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	// Default session gap is 2h and default limit is 20.
	if !strings.Contains(out, "2h0m0s") {
		t.Errorf("empty config should show default gap 2h0m0s; got: %q", out)
	}
	if !strings.Contains(out, "20") {
		t.Errorf("empty config should show default limit 20; got: %q", out)
	}
	if !strings.Contains(out, "none configured") {
		t.Errorf("empty config should note no roots; got: %q", out)
	}
}

// TestConfigLimitAppliesToFind verifies that a configured limit becomes the
// default cap for a list command while an explicit flag still overrides it.
func TestConfigLimitAppliesToFind(t *testing.T) {
	root := t.TempDir()
	// Create five files that all land in the same broad window.
	for i := 0; i < 5; i++ {
		f := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	db := filepath.Join(t.TempDir(), "index.db")
	withConfig(t, `{"limit":2}`)

	// Index (explicit --db beats config db, which is empty here anyway).
	if _, err := execute(t, "--db", db, "index", root); err != nil {
		t.Fatalf("index: %v", err)
	}

	// find with the configured limit of 2 should cap results at 2.
	out, err := execute(t, "--db", db, "find", "this year", "--json")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	var res struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("parse find json: %v (out=%q)", err, out)
	}
	if res.Count > 2 {
		t.Errorf("configured limit 2 should cap find at 2; got count=%d", res.Count)
	}

	// An explicit --limit overrides the configured default.
	out, err = execute(t, "--db", db, "find", "this year", "--json", "--limit", "5")
	if err != nil {
		t.Fatalf("find --limit: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("parse find json: %v (out=%q)", err, out)
	}
	if res.Count < 3 {
		t.Errorf("explicit --limit 5 should override configured 2; got count=%d", res.Count)
	}
}

// TestConfigRootsFeedIndex verifies that `index` with no positional args uses
// the roots from the config file.
func TestConfigRootsFeedIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	db := filepath.Join(t.TempDir(), "index.db")

	// JSON-encode the root so Windows backslashes stay valid in the file.
	rootsJSON, _ := json.Marshal([]string{root})
	withConfig(t, `{"roots":`+string(rootsJSON)+`}`)

	out, err := execute(t, "--db", db, "index")
	if err != nil {
		t.Fatalf("index with configured roots: %v", err)
	}
	if !strings.Contains(out, "1 files seen") && !strings.Contains(out, "files seen") {
		t.Errorf("index should have scanned the configured root; got: %q", out)
	}

	// Confirm the file actually made it into the index via stats.
	out, err = execute(t, "--db", db, "stats", "--json")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, `"files": 1`) {
		t.Errorf("stats should report 1 indexed file; got: %q", out)
	}
}

func TestBadConfigSurfacesError(t *testing.T) {
	withConfig(t, `{"gap":"garbage"}`)
	// Any command should surface the config parse error via PersistentPreRunE.
	if _, err := execute(t, "config", "show"); err == nil {
		t.Error("a malformed config should make commands error")
	}
}
