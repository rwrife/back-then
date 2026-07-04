package walk

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeTree materializes a map of relative-path -> content under a fresh temp
// dir and returns the root. Directories are created as needed. A path ending
// in "/" creates an (otherwise empty) directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// walkRel returns the sorted set of relative (forward-slashed) file paths a
// walk emits for root with the given options.
func walkRel(t *testing.T, root string, opts Options) []string {
	t.Helper()
	var got []string
	err := Walk([]string{root}, opts, func(s FileSignal) error {
		rel, rerr := filepath.Rel(root, s.Path)
		if rerr != nil {
			t.Fatalf("rel: %v", rerr)
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(got)
	return got
}

func assertFiles(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("walked files =\n  %v\nwant\n  %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("walked files =\n  %v\nwant\n  %v", got, want)
		}
	}
}

func TestIgnoreFileBasename(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "*.log\nsecret.txt\n",
		"keep.txt":        "k",
		"app.log":         "l",
		"secret.txt":      "s",
		"sub/nested.log":  "n",
		"sub/ok.md":       "o",
	})
	got := walkRel(t, root, Options{})
	// *.log and secret.txt ignored at any depth (basename match); the ignore
	// file itself is a regular file and is not special-cased, so it appears.
	assertFiles(t, got, []string{".backthenignore", "keep.txt", "sub/ok.md"})
}

func TestIgnoreFileDirOnly(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "cache/\n",
		"a.txt":           "a",
		"cache/x.bin":     "x",
		"cache/y.bin":     "y",
		"src/cache.txt":   "c", // a file named cache -> NOT a dir, must survive
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "a.txt", "src/cache.txt"})
}

func TestIgnoreFileAnchored(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "/out\n",
		"out/thing.o":     "o", // anchored: this out/ at root is ignored
		"src/out/keep":    "k", // deeper out/ NOT anchored to root -> kept
		"top.txt":         "t",
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "src/out/keep", "top.txt"})
}

func TestIgnoreFilePathPattern(t *testing.T) {
	// A pattern containing a slash (but no leading slash) is anchored to the
	// ignore file's directory and matched against the relative path.
	root := writeTree(t, map[string]string{
		".backthenignore": "logs/*.txt\n",
		"logs/a.txt":      "a", // matches
		"logs/b.md":       "b", // different ext -> kept
		"other/c.txt":     "c", // different dir -> kept
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "logs/b.md", "other/c.txt"})
}

func TestIgnoreFileNegation(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "*.log\n!keep.log\n",
		"a.log":           "a", // ignored
		"keep.log":        "k", // re-included by negation (last match wins)
		"b.txt":           "b",
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "b.txt", "keep.log"})
}

func TestIgnoreFileNestedOverride(t *testing.T) {
	// A deeper .backthenignore can re-include something a shallower one ignored,
	// because deeper scopes are evaluated last and last match wins.
	root := writeTree(t, map[string]string{
		".backthenignore":     "*.tmp\n",
		"a.tmp":               "a", // ignored by root scope
		"sub/.backthenignore": "!important.tmp\n",
		"sub/important.tmp":   "i", // re-included by nested scope
		"sub/other.tmp":       "o", // still ignored by inherited root scope
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{
		".backthenignore",
		"sub/.backthenignore",
		"sub/important.tmp",
	})
}

func TestIgnoreFileSiblingIsolation(t *testing.T) {
	// A .backthenignore in one subdirectory must not affect a sibling.
	root := writeTree(t, map[string]string{
		"one/.backthenignore": "*.dat\n",
		"one/x.dat":           "x", // ignored (one/ scope)
		"one/x.txt":           "t",
		"two/y.dat":           "y", // NOT ignored (two/ has no scope)
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{
		"one/.backthenignore",
		"one/x.txt",
		"two/y.dat",
	})
}

func TestIgnoreFileCommentsAndBlanks(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "# a comment\n\n   \n*.bak\n",
		"a.bak":           "a",
		"b.txt":           "b",
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "b.txt"})
}

func TestIgnoreFileDisabled(t *testing.T) {
	root := writeTree(t, map[string]string{
		".backthenignore": "*.log\n",
		"a.log":           "a",
		"b.txt":           "b",
	})
	got := walkRel(t, root, Options{NoIgnoreFile: true})
	// With ignore files disabled, nothing is pruned by them.
	assertFiles(t, got, []string{".backthenignore", "a.log", "b.txt"})
}

func TestIgnoreFileDefaultSkipStillApplies(t *testing.T) {
	// Default skip list and ignore files are independent; both prune.
	root := writeTree(t, map[string]string{
		".backthenignore":     "*.log\n",
		"a.log":               "a", // ignored by ignore file
		"keep.txt":            "k",
		"node_modules/dep.js": "d", // pruned by default skip list
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "keep.txt"})
}

func TestIgnoreFileRootDirIgnorePrunesSubtree(t *testing.T) {
	// A directory match must prune the whole subtree, including nested files
	// that individually would not match the pattern.
	root := writeTree(t, map[string]string{
		".backthenignore":   "tmpdir/\n",
		"tmpdir/deep/a.txt": "a", // pruned via ancestor dir match
		"tmpdir/b.md":       "b", // pruned
		"keep.txt":          "k",
	})
	got := walkRel(t, root, Options{})
	assertFiles(t, got, []string{".backthenignore", "keep.txt"})
}

func TestCompilePatternSkipsNoise(t *testing.T) {
	for _, line := range []string{"", "   ", "# comment", "!", "/"} {
		if _, ok := compilePattern(line); ok {
			t.Errorf("compilePattern(%q) = ok, want skipped", line)
		}
	}
	// `!out/` negates and is directory-only; with no internal slash it matches
	// by base name at any depth, so it is not anchored.
	p, ok := compilePattern("!out/")
	if !ok {
		t.Fatal("compilePattern(!out/) should compile")
	}
	if !p.negate || !p.dirOnly || p.anchored || p.pattern != "out" {
		t.Errorf("compilePattern(!out/) = %+v, want negate+dirOnly, unanchored, pattern=out", p)
	}
	// A rooted, nested pattern is anchored and matched against the relative path.
	p2, ok := compilePattern("/logs/*.tmp")
	if !ok {
		t.Fatal("compilePattern(/logs/*.tmp) should compile")
	}
	if p2.negate || p2.dirOnly || !p2.anchored || p2.pattern != "logs/*.tmp" {
		t.Errorf("compilePattern(/logs/*.tmp) = %+v, want anchored pattern=logs/*.tmp", p2)
	}
}
