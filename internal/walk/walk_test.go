package walk

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// buildTree creates a small fixture directory tree under a fresh temp dir and
// returns its root. Layout:
//
//	root/
//	  a.txt
//	  b.MD            (uppercase ext -> should normalize to .md)
//	  no_ext
//	  sub/
//	    c.pdf
//	  node_modules/   (default-skipped)
//	    junk.js
//	  .git/           (default-skipped)
//	    config
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	mustWrite := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("a.txt", "alpha")
	mustWrite("b.MD", "bravo")
	mustWrite("no_ext", "x")
	mustWrite("sub/c.pdf", "charlie")
	mustWrite("node_modules/junk.js", "skip me")
	mustWrite(".git/config", "skip me too")

	return root
}

func collect(t *testing.T, root string, opts Options) map[string]FileSignal {
	t.Helper()
	got := map[string]FileSignal{}
	err := Walk([]string{root}, opts, func(s FileSignal) error {
		rel, rerr := filepath.Rel(root, s.Path)
		if rerr != nil {
			t.Fatalf("rel: %v", rerr)
		}
		got[filepath.ToSlash(rel)] = s
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return got
}

func TestWalkSkipsDefaultDirs(t *testing.T) {
	root := buildTree(t)
	got := collect(t, root, Options{})

	want := []string{"a.txt", "b.MD", "no_ext", "sub/c.pdf"}
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(want)

	if len(keys) != len(want) {
		t.Fatalf("walked files = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("walked files = %v, want %v", keys, want)
		}
	}
}

func TestWalkExtractsSignals(t *testing.T) {
	root := buildTree(t)
	got := collect(t, root, Options{})

	a, ok := got["a.txt"]
	if !ok {
		t.Fatal("a.txt missing from walk results")
	}
	if a.Ext != ".txt" {
		t.Errorf("a.txt ext = %q, want .txt", a.Ext)
	}
	if a.Size != int64(len("alpha")) {
		t.Errorf("a.txt size = %d, want %d", a.Size, len("alpha"))
	}
	if a.ModTime.IsZero() {
		t.Error("a.txt ModTime is zero")
	}
	if !filepath.IsAbs(a.Path) {
		t.Errorf("a.txt path %q is not absolute", a.Path)
	}
	if a.ParentDir != filepath.Dir(a.Path) {
		t.Errorf("a.txt ParentDir = %q, want %q", a.ParentDir, filepath.Dir(a.Path))
	}

	// Uppercase extension must be normalized to lowercase.
	if b := got["b.MD"]; b.Ext != ".md" {
		t.Errorf("b.MD ext = %q, want .md (lowercased)", b.Ext)
	}

	// Extensionless file must have an empty Ext, not a panic.
	if n := got["no_ext"]; n.Ext != "" {
		t.Errorf("no_ext ext = %q, want empty", n.Ext)
	}
}

func TestWalkExtraSkip(t *testing.T) {
	root := buildTree(t)
	got := collect(t, root, Options{ExtraSkipDirs: []string{"sub"}})

	if _, ok := got["sub/c.pdf"]; ok {
		t.Error("sub/c.pdf should be skipped when 'sub' is in ExtraSkipDirs")
	}
	if _, ok := got["a.txt"]; !ok {
		t.Error("a.txt should still be present")
	}
}

func TestWalkVisitErrorAborts(t *testing.T) {
	root := buildTree(t)
	sentinel := os.ErrClosed
	err := Walk([]string{root}, Options{}, func(FileSignal) error {
		return sentinel
	})
	if err == nil {
		t.Fatal("expected Walk to propagate visit error, got nil")
	}
}

func TestWalkMissingRoot(t *testing.T) {
	// A non-existent root should not crash; WalkDir reports the error to the
	// callback which we swallow for directories, yielding no files and no error.
	got := map[string]FileSignal{}
	err := Walk([]string{filepath.Join(t.TempDir(), "nope")}, Options{}, func(s FileSignal) error {
		got[s.Path] = s
		return nil
	})
	if err != nil {
		t.Fatalf("Walk on missing root returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no files for missing root, got %d", len(got))
	}
}
