package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/walk"
	"github.com/rwrife/back-then/internal/when"
)

// seedTimed writes files with explicit mod times into a temp tree, indexes
// them into a fresh store, and returns the store. It lets Forget/CountInWindow
// tests pin files to known instants so window math is deterministic.
func seedTimed(t *testing.T, files map[string]time.Time) *Store {
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
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return s
}

// TestCountAndForgetWindow verifies that CountInWindow reports exactly the
// files whose effective time lands in [Start,End) and that Forget deletes
// precisely those, leaving out-of-window files intact.
func TestCountAndForgetWindow(t *testing.T) {
	y2019 := time.Date(2019, 6, 15, 12, 0, 0, 0, time.UTC)
	y2020 := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)
	y2021 := time.Date(2021, 6, 15, 12, 0, 0, 0, time.UTC)

	s := seedTimed(t, map[string]time.Time{
		"a.txt": y2019,
		"b.txt": y2020,
		"c.txt": y2020.Add(2 * time.Hour),
		"d.txt": y2021,
	})

	// Window covering all of 2020.
	w := when.Window{
		Start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	n, err := s.CountInWindow(w)
	if err != nil {
		t.Fatalf("CountInWindow: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountInWindow(2020) = %d, want 2 (b,c)", n)
	}

	removed, err := s.Forget(w)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if removed != 2 {
		t.Fatalf("Forget(2020) removed %d, want 2", removed)
	}

	// The 2019 and 2021 files must survive.
	st, err := s.Stats(10)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Files != 2 {
		t.Fatalf("after Forget, Files = %d, want 2 (a,d)", st.Files)
	}
	if _, ok, _ := s.FileByPathSuffix(t, "a.txt"); !ok {
		t.Errorf("a.txt (2019) should survive forgetting 2020")
	}
	if _, ok, _ := s.FileByPathSuffix(t, "d.txt"); !ok {
		t.Errorf("d.txt (2021) should survive forgetting 2020")
	}
	if _, ok, _ := s.FileByPathSuffix(t, "b.txt"); ok {
		t.Errorf("b.txt (2020) should have been forgotten")
	}
}

// TestForgetEmptyWindow confirms forgetting a range with no indexed files is a
// harmless no-op that removes nothing.
func TestForgetEmptyWindow(t *testing.T) {
	s := seedTimed(t, map[string]time.Time{
		"recent.txt": time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	w := when.Window{
		Start: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if n, err := s.CountInWindow(w); err != nil || n != 0 {
		t.Fatalf("CountInWindow(empty) = %d, err %v; want 0,nil", n, err)
	}
	removed, err := s.Forget(w)
	if err != nil {
		t.Fatalf("Forget(empty): %v", err)
	}
	if removed != 0 {
		t.Errorf("Forget(empty) removed %d, want 0", removed)
	}
}

// FileByPathSuffix is a test-only helper: it looks up a file whose path ends
// with suffix. It keeps the window tests readable without hard-coding temp
// paths.
func (s *Store) FileByPathSuffix(t *testing.T, suffix string) (FileRecord, bool, error) {
	t.Helper()
	all, err := s.AllFiles()
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	for _, f := range all {
		if len(f.Path) >= len(suffix) && f.Path[len(f.Path)-len(suffix):] == suffix {
			return f, true, nil
		}
	}
	return FileRecord{}, false, nil
}
