package export

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/store"
)

// writeFixture creates a source file with content and returns its abs path.
func writeFixture(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func rec(path string, size int64) store.FileRecord {
	return store.FileRecord{
		Path:    path,
		Size:    size,
		Ext:     filepath.Ext(path),
		ModTime: time.Now(),
	}
}

func TestExportFolderPreserve(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	a := writeFixture(t, src, "docs/a.txt", "alpha")
	b := writeFixture(t, src, "docs/sub/b.txt", "bravo")

	res, err := Run(Options{
		Files: []store.FileRecord{rec(a, 5), rec(b, 5)},
		Dest:  dst,
		Name:  "back-then-2024-01-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}
	if res.TotalBytes != 10 {
		t.Fatalf("expected 10 total bytes, got %d", res.TotalBytes)
	}

	// Both files exist under the bundle, preserving their source subtree tail.
	for _, e := range res.Entries {
		got := filepath.Join(res.Bundle, filepath.FromSlash(e.Dest))
		if _, err := os.Stat(got); err != nil {
			t.Fatalf("expected exported file %q: %v", got, err)
		}
	}
	// Originals untouched.
	if _, err := os.Stat(a); err != nil {
		t.Fatalf("original was removed: %v", err)
	}
}

func TestExportZip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	a := writeFixture(t, src, "a.txt", "alpha")
	b := writeFixture(t, src, "b.txt", "bravo")

	res, err := Run(Options{
		Files:  []store.FileRecord{rec(a, 5), rec(b, 5)},
		Dest:   dst,
		Name:   "bundle",
		Zip:    true,
		Layout: LayoutFlat,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(res.Bundle) != ".zip" {
		t.Fatalf("expected .zip bundle, got %q", res.Bundle)
	}
	zr, err := zip.OpenReader(res.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{"bundle/a.txt", "bundle/b.txt"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("zip contents = %v, want %v", names, want)
	}
}

func TestExportFlatCollision(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	// Two files with the same base name from different folders.
	a := writeFixture(t, src, "x/report.pdf", "one")
	b := writeFixture(t, src, "y/report.pdf", "two")

	res, err := Run(Options{
		Files:  []store.FileRecord{rec(a, 3), rec(b, 3)},
		Dest:   dst,
		Name:   "flat",
		Layout: LayoutFlat,
	})
	if err != nil {
		t.Fatal(err)
	}
	dests := []string{res.Entries[0].Dest, res.Entries[1].Dest}
	sort.Strings(dests)
	if dests[0] != "report (2).pdf" || dests[1] != "report.pdf" {
		t.Fatalf("collision handling wrong: %v", dests)
	}
	// Both files really landed.
	for _, e := range res.Entries {
		if _, err := os.Stat(filepath.Join(res.Bundle, e.Dest)); err != nil {
			t.Fatalf("missing %q: %v", e.Dest, err)
		}
	}
}

func TestExportDryRunWritesNothing(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	a := writeFixture(t, src, "a.txt", "alpha")

	res, err := Run(Options{
		Files:  []store.FileRecord{rec(a, 5)},
		Dest:   dst,
		Name:   "preview",
		DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.TotalBytes != 5 || len(res.Entries) != 1 {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
	if _, err := os.Stat(res.Bundle); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote a bundle at %q", res.Bundle)
	}
}

func TestExportRefusesExistingWithoutForce(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	a := writeFixture(t, src, "a.txt", "alpha")

	// Pre-create the destination bundle.
	existing := filepath.Join(dst, "back-then-dup")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := Options{Files: []store.FileRecord{rec(a, 5)}, Dest: dst, Name: "back-then-dup"}
	if _, err := Run(opts); err == nil {
		t.Fatal("expected error exporting over existing destination without --force")
	}

	opts.Force = true
	if _, err := Run(opts); err != nil {
		t.Fatalf("--force export failed: %v", err)
	}
}
