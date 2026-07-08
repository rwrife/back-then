package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfig writes body to a temp file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestLoadMissingFileIsZeroConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope", FileName)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if cfg.DB != "" || len(cfg.Roots) != 0 || cfg.Gap != 0 || cfg.LimitSet {
		t.Errorf("missing file should yield zero Config, got %+v", cfg)
	}
}

func TestLoadFullConfig(t *testing.T) {
	p := writeConfig(t, `{
	  "db": "/data/index.db",
	  "roots": ["/home/me/pics", "/home/me/docs"],
	  "gap": "90m",
	  "limit": 5
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DB != "/data/index.db" {
		t.Errorf("DB = %q, want /data/index.db", cfg.DB)
	}
	if len(cfg.Roots) != 2 || cfg.Roots[0] != "/home/me/pics" || cfg.Roots[1] != "/home/me/docs" {
		t.Errorf("Roots = %v, want two roots", cfg.Roots)
	}
	if cfg.Gap != 90*time.Minute {
		t.Errorf("Gap = %s, want 90m", cfg.Gap)
	}
	if !cfg.LimitSet || cfg.Limit != 5 {
		t.Errorf("Limit = %d (set=%v), want 5 (set)", cfg.Limit, cfg.LimitSet)
	}
}

func TestLoadEmptyObjectIsZeroButValid(t *testing.T) {
	p := writeConfig(t, `{}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LimitSet {
		t.Error("empty object should leave LimitSet false")
	}
	if cfg.Gap != 0 {
		t.Errorf("empty object should leave Gap zero, got %s", cfg.Gap)
	}
}

func TestLoadLimitZeroIsDistinguishedFromUnset(t *testing.T) {
	p := writeConfig(t, `{"limit": 0}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LimitSet {
		t.Error("explicit limit:0 should set LimitSet true")
	}
	if cfg.Limit != 0 {
		t.Errorf("Limit = %d, want 0", cfg.Limit)
	}
}

func TestLoadRejectsBadJSON(t *testing.T) {
	p := writeConfig(t, `{"db": }`)
	if _, err := Load(p); err == nil {
		t.Error("malformed JSON should error")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	p := writeConfig(t, `{"nope": true}`)
	if _, err := Load(p); err == nil {
		t.Error("unknown key should error so typos surface")
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	p := writeConfig(t, `{"gap": "not-a-duration"}`)
	if _, err := Load(p); err == nil {
		t.Error("unparseable gap should error")
	}
}

func TestLoadRejectsNegativeDuration(t *testing.T) {
	p := writeConfig(t, `{"gap": "-5m"}`)
	if _, err := Load(p); err == nil {
		t.Error("negative gap should error")
	}
}

func TestDefaultPath(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if filepath.Base(got) != FileName {
		t.Errorf("DefaultPath base = %q, want %q", filepath.Base(got), FileName)
	}
	if filepath.Base(filepath.Dir(got)) != "back-then" {
		t.Errorf("DefaultPath should live under a back-then dir, got %q", got)
	}
}
