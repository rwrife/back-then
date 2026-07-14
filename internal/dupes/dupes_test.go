package dupes

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/store"
)

func rec(path string, size int64, mod time.Time) store.FileRecord {
	return store.FileRecord{
		Path:    path,
		Size:    size,
		Ext:     filepath.Ext(path),
		ModTime: mod,
	}
}

func TestFindGroupsSameSizeAndExt(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	files := []store.FileRecord{
		rec("/a/report.pdf", 2048, base),
		rec("/b/report (1).pdf", 2048, base.Add(time.Minute)), // dupe of report.pdf
		rec("/a/photo.jpg", 5000, base),                       // unique size
		rec("/c/notes.txt", 100, base),                        // unique
		rec("/d/report.txt", 2048, base),                      // same size, different ext -> not a dupe
	}

	groups := Find(files, Options{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 dupe group, got %d: %+v", len(groups), groups)
	}
	g := groups[0]
	if g.Count() != 2 {
		t.Fatalf("expected 2 files in group, got %d", g.Count())
	}
	if g.Size != 2048 {
		t.Fatalf("expected size 2048, got %d", g.Size)
	}
	if g.Wasted() != 2048 {
		t.Fatalf("expected wasted 2048, got %d", g.Wasted())
	}
	// Keeper is the oldest.
	if g.Files[0].Path != "/a/report.pdf" {
		t.Fatalf("expected keeper /a/report.pdf, got %s", g.Files[0].Path)
	}
	if got := len(g.Dupes()); got != 1 {
		t.Fatalf("expected 1 dupe copy, got %d", got)
	}
}

func TestFindSkipsZeroByteFiles(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	files := []store.FileRecord{
		rec("/a/empty1.log", 0, base),
		rec("/a/empty2.log", 0, base),
	}
	if groups := Find(files, Options{}); len(groups) != 0 {
		t.Fatalf("zero-byte files must not group, got %d", len(groups))
	}
}

func TestFindMinSize(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	files := []store.FileRecord{
		rec("/a/x.bin", 10, base),
		rec("/b/y.bin", 10, base),
	}
	if groups := Find(files, Options{MinSize: 100}); len(groups) != 0 {
		t.Fatalf("files below min-size must be skipped, got %d", len(groups))
	}
	if groups := Find(files, Options{MinSize: 5}); len(groups) != 1 {
		t.Fatalf("files above min-size must group, got %d", len(groups))
	}
}

func TestFindGapSplitsClusters(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	files := []store.FileRecord{
		rec("/a/1.dat", 500, base),
		rec("/b/2.dat", 500, base.Add(time.Minute)),  // within gap of #1
		rec("/c/3.dat", 500, base.Add(48*time.Hour)), // far away
	}
	// A 1h gap should split #3 into its own (singleton) cluster, leaving one
	// 2-file dupe group.
	groups := Find(files, Options{Gap: int64(time.Hour)})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after gap split, got %d", len(groups))
	}
	if groups[0].Count() != 2 {
		t.Fatalf("expected 2 files in the surviving group, got %d", groups[0].Count())
	}
}

func TestVerifyConfirmsAndSplitsByContent(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Two identical files and one same-size-but-different-content file.
	same := []byte("hello duplicate world!!")  // 23 bytes
	other := []byte("XXXXXXXXXXXXXXXXXXXXXXX") // also 23 bytes, different content
	if len(same) != len(other) {
		t.Fatalf("fixture sizes must match: %d vs %d", len(same), len(other))
	}

	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.txt")
	pc := filepath.Join(dir, "c.txt")
	for p, data := range map[string][]byte{pa: same, pb: same, pc: other} {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := []store.FileRecord{
		rec(pa, int64(len(same)), base),
		rec(pb, int64(len(same)), base.Add(time.Minute)),
		rec(pc, int64(len(other)), base.Add(2*time.Minute)),
	}

	// Metadata pass groups all three (same size + ext).
	meta := Find(files, Options{})
	if len(meta) != 1 || meta[0].Count() != 3 {
		t.Fatalf("expected 1 metadata group of 3, got %+v", meta)
	}

	// Verify must split off the odd file, leaving a confirmed 2-file group.
	verified, err := Verify(meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified) != 1 {
		t.Fatalf("expected 1 verified group, got %d", len(verified))
	}
	if verified[0].Count() != 2 {
		t.Fatalf("expected 2 confirmed dupes, got %d", verified[0].Count())
	}
	if verified[0].Hash == "" {
		t.Fatalf("verified group should carry a content hash")
	}
	if verified[0].Wasted() != int64(len(same)) {
		t.Fatalf("expected wasted %d, got %d", len(same), verified[0].Wasted())
	}
}

func TestTotalWasted(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	files := []store.FileRecord{
		rec("/a/x.iso", 1000, base),
		rec("/b/x.iso", 1000, base),
		rec("/c/x.iso", 1000, base), // 2 wasted copies * 1000
		rec("/a/y.zip", 40, base),
		rec("/b/y.zip", 40, base), // 1 wasted copy * 40
	}
	groups := Find(files, Options{})
	if got := TotalWasted(groups); got != 2040 {
		t.Fatalf("expected total wasted 2040, got %d", got)
	}
	// Largest-wasted group must sort first.
	if groups[0].Size != 1000 {
		t.Fatalf("expected largest group first, got size %d", groups[0].Size)
	}
}
