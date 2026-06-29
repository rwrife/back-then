package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/walk"
)

// fixtureTree writes a few files into a temp dir and returns the root.
func fixtureTree(t *testing.T) string {
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

func openTemp(t *testing.T) *Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesSchema(t *testing.T) {
	s := openTemp(t)
	st, err := s.Stats(10)
	if err != nil {
		t.Fatalf("Stats on fresh db: %v", err)
	}
	if st.Files != 0 {
		t.Errorf("fresh index Files = %d, want 0", st.Files)
	}
}

func TestIndexAndIncremental(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)

	// First index: 3 files (node_modules skipped), all new.
	res, err := s.Index([]string{root}, walk.Options{})
	if err != nil {
		t.Fatalf("first Index: %v", err)
	}
	if res.Seen != 3 || res.Upserted != 3 || res.Skipped != 0 {
		t.Fatalf("first Index = %+v, want seen=3 upserted=3 skipped=0", res)
	}

	// Second index, nothing changed: everything skipped.
	res, err = s.Index([]string{root}, walk.Options{})
	if err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if res.Seen != 3 || res.Upserted != 0 || res.Skipped != 3 {
		t.Fatalf("second Index = %+v, want seen=3 upserted=0 skipped=3", res)
	}
}

func TestIndexDetectsChange(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatal(err)
	}

	// Modify a file's size and bump its mod time deterministically.
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("alpha-plus-more"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatal(err)
	}

	res, err := s.Index([]string{root}, walk.Options{})
	if err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	if res.Upserted != 1 || res.Skipped != 2 {
		t.Fatalf("re-Index = %+v, want upserted=1 skipped=2", res)
	}
}

func TestStatsReflectsIndex(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats(10)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Files != 3 {
		t.Errorf("Stats.Files = %d, want 3", st.Files)
	}
	wantSize := int64(len("alpha") + len("bravo") + len("charlie"))
	if st.TotalSize != wantSize {
		t.Errorf("Stats.TotalSize = %d, want %d", st.TotalSize, wantSize)
	}
	if st.Oldest.IsZero() || st.Newest.IsZero() {
		t.Error("Stats date span should be populated")
	}

	// .txt (2) should rank above .md (1).
	if len(st.TopExts) < 2 {
		t.Fatalf("TopExts = %v, want at least 2 entries", st.TopExts)
	}
	if st.TopExts[0].Ext != ".txt" || st.TopExts[0].Count != 2 {
		t.Errorf("TopExts[0] = %+v, want {.txt 2}", st.TopExts[0])
	}
}

func TestStatsTopNLimit(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopExts) != 1 {
		t.Errorf("with top=1, TopExts len = %d, want 1", len(st.TopExts))
	}
}

func TestReopenPersists(t *testing.T) {
	root := fixtureTree(t)
	dbPath := filepath.Join(t.TempDir(), "index.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen the same file: contents and incremental state must persist.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	st, err := s2.Stats(10)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 3 {
		t.Errorf("after reopen Files = %d, want 3", st.Files)
	}
	res, err := s2.Index([]string{root}, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 3 {
		t.Errorf("after reopen incremental Skipped = %d, want 3", res.Skipped)
	}
}
