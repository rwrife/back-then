package store

import (
	"testing"

	"github.com/rwrife/back-then/internal/walk"
)

// TestAllFilesOrdered indexes a fixture tree and checks AllFiles returns every
// (non-skipped) file, ordered by effective time ascending.
func TestAllFilesOrdered(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	files, err := s.AllFiles()
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	// fixtureTree writes 3 indexable files (node_modules skipped).
	if len(files) != 3 {
		t.Fatalf("AllFiles returned %d files, want 3", len(files))
	}
	// Effective times must be non-decreasing.
	for i := 1; i < len(files); i++ {
		if files[i].EffectiveTime().Before(files[i-1].EffectiveTime()) {
			t.Errorf("AllFiles not ordered by effective time at index %d", i)
		}
	}
	// Every record must carry a mod time (always available).
	for _, f := range files {
		if f.ModTime.IsZero() {
			t.Errorf("file %s has zero ModTime", f.Path)
		}
	}
}

// TestFileByPath verifies exact-path lookup, including the not-found case.
func TestFileByPath(t *testing.T) {
	root := fixtureTree(t)
	s := openTemp(t)
	if _, err := s.Index([]string{root}, walk.Options{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	all, err := s.AllFiles()
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	want := all[0]

	got, ok, err := s.FileByPath(want.Path)
	if err != nil {
		t.Fatalf("FileByPath: %v", err)
	}
	if !ok {
		t.Fatalf("FileByPath(%q) not found, want found", want.Path)
	}
	if got.Path != want.Path || got.Size != want.Size {
		t.Errorf("FileByPath = %+v, want %+v", got, want)
	}

	if _, ok, err := s.FileByPath("/definitely/not/indexed.txt"); err != nil || ok {
		t.Errorf("FileByPath(missing) = ok:%v err:%v, want ok:false err:nil", ok, err)
	}
}
