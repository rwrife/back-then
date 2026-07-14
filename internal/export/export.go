// Package export bundles the files of a session or time window into a dated
// destination — a folder or a single .zip — closing the loop from "back-then
// found it" to "I have it all in one place."
//
// Guarantees:
//   - Copy-only: originals are never moved or modified; export only reads.
//   - Collision-safe: when two source files would land on the same destination
//     name (common with --flat, or across roots), later ones are disambiguated
//     with a numeric suffix rather than silently overwriting.
//   - No clobber: an existing destination folder or zip is refused unless the
//     caller opts into Force.
//
// The package is pure library code (no cobra); the CLI wires flags to it.
package export

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rwrife/back-then/internal/store"
)

// Layout controls how source paths map to destination paths.
type Layout int

const (
	// LayoutPreserve keeps each file under a relative path derived from its
	// source (the tail of its absolute path, drive/leading separators
	// stripped), so the original directory structure is recognizable.
	LayoutPreserve Layout = iota
	// LayoutFlat drops every file directly into the destination root, using
	// just its base name (with collision-safe suffixes as needed).
	LayoutFlat
)

// Options configures an export run.
type Options struct {
	// Files are the source records to export, in the order they should be
	// considered (collision suffixes follow this order deterministically).
	Files []store.FileRecord
	// Dest is the destination: a directory that will contain the bundle
	// folder, or — when Zip is true — the path the .zip is written to.
	//
	// For folder mode the bundle is created at Dest/<Name>. For zip mode the
	// archive is written at Dest/<Name>.zip.
	Dest string
	// Name is the bundle label (e.g. "back-then-20240115-0930" or
	// "back-then-2024-spring"). It becomes the folder/zip name and the zip's
	// internal top-level directory.
	Name string
	// Layout selects preserve vs flat.
	Layout Layout
	// Zip writes a single .zip instead of a folder tree.
	Zip bool
	// Force allows overwriting an existing destination folder/zip.
	Force bool
	// DryRun computes the manifest (and total bytes) without writing anything.
	DryRun bool
}

// Entry is one planned or performed copy in the manifest.
type Entry struct {
	// Source is the absolute source path.
	Source string `json:"source"`
	// Dest is the path relative to the bundle root (forward-slashed for
	// stable, cross-platform JSON/zip semantics).
	Dest string `json:"dest"`
	// Size is the source file size in bytes.
	Size int64 `json:"size_bytes"`
}

// Result summarizes an export (or a dry run).
type Result struct {
	// Bundle is the resolved output path: the folder or the .zip file. Empty
	// for a dry run that did not write.
	Bundle string `json:"bundle"`
	// Entries is the ordered manifest of exported files.
	Entries []Entry `json:"entries"`
	// TotalBytes is the sum of exported file sizes.
	TotalBytes int64 `json:"total_bytes"`
	// DryRun echoes whether this was a preview.
	DryRun bool `json:"dry_run"`
	// Zip echoes the output mode.
	Zip bool `json:"zip"`
}

// Run plans the export, then (unless DryRun) writes it. It never mutates
// source files. The returned Result carries the manifest and totals for both
// real and dry runs.
func Run(opts Options) (Result, error) {
	res := Result{DryRun: opts.DryRun, Zip: opts.Zip}

	if opts.Name == "" {
		return res, fmt.Errorf("export: empty bundle name")
	}
	if opts.Dest == "" {
		return res, fmt.Errorf("export: empty destination")
	}

	res.Entries = plan(opts.Files, opts.Layout)
	for _, e := range res.Entries {
		res.TotalBytes += e.Size
	}

	// Resolve the concrete output path so callers can report it even on a dry
	// run ("this is what I would create").
	bundle := filepath.Join(opts.Dest, opts.Name)
	if opts.Zip {
		bundle += ".zip"
	}
	res.Bundle = bundle

	if opts.DryRun {
		// Nothing written; leave Bundle set so the preview names the target.
		return res, nil
	}

	if err := ensureWritableDest(bundle, opts.Force); err != nil {
		return res, err
	}

	if opts.Zip {
		if err := writeZip(bundle, opts.Name, res.Entries); err != nil {
			return res, err
		}
	} else {
		if err := writeFolder(bundle, res.Entries); err != nil {
			return res, err
		}
	}
	return res, nil
}

// plan maps each source file to a collision-safe destination path under the
// bundle root, honoring the chosen layout. It is deterministic: the same input
// order always yields the same destinations.
func plan(files []store.FileRecord, layout Layout) []Entry {
	entries := make([]Entry, 0, len(files))
	used := map[string]bool{}
	for _, f := range files {
		rel := destRel(f.Path, layout)
		rel = dedupe(rel, used)
		used[strings.ToLower(rel)] = true
		entries = append(entries, Entry{Source: f.Path, Dest: rel, Size: f.Size})
	}
	// Stable, human-friendly manifest order: by destination path.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Dest < entries[j].Dest
	})
	return entries
}

// destRel derives the initial (pre-dedupe) destination path for a source.
func destRel(src string, layout Layout) string {
	if layout == LayoutFlat {
		return filepath.Base(src)
	}
	// Preserve mode: strip the volume name (Windows drive) and any leading
	// separators so the source tree becomes a clean relative subtree.
	rel := src
	if vol := filepath.VolumeName(rel); vol != "" {
		rel = rel[len(vol):]
	}
	rel = strings.TrimLeft(rel, `/\`)
	if rel == "" {
		rel = filepath.Base(src)
	}
	return filepath.ToSlash(rel)
}

// dedupe returns rel unchanged if unused, else inserts a " (n)" counter before
// the extension until the (case-insensitive) name is free. Case-insensitive so
// exports remain safe on case-preserving-but-insensitive filesystems.
func dedupe(rel string, used map[string]bool) string {
	if !used[strings.ToLower(rel)] {
		return rel
	}
	ext := path_Ext(rel)
	stem := rel[:len(rel)-len(ext)]
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if !used[strings.ToLower(cand)] {
			return cand
		}
	}
}

// path_Ext returns the extension of a forward-slashed path (filepath.Ext keys
// off the last element only, which is what we want, but we normalize on '/').
func path_Ext(p string) string {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	if j := strings.LastIndex(base, "."); j > 0 {
		return base[j:]
	}
	return ""
}

// ensureWritableDest refuses to clobber an existing destination unless force is
// set, and ensures the parent directory exists.
func ensureWritableDest(bundle string, force bool) error {
	if _, err := os.Stat(bundle); err == nil {
		if !force {
			return fmt.Errorf("destination %q already exists; pass --force to overwrite", bundle)
		}
		if err := os.RemoveAll(bundle); err != nil {
			return fmt.Errorf("remove existing destination: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	return nil
}

// writeFolder copies each entry into bundle/<Entry.Dest>, creating parent
// directories as needed. Sources are read-only; originals are untouched.
func writeFolder(bundle string, entries []Entry) error {
	for _, e := range entries {
		dst := filepath.Join(bundle, filepath.FromSlash(e.Dest))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create dir for %q: %w", e.Dest, err)
		}
		if err := copyFile(e.Source, dst); err != nil {
			return err
		}
	}
	return nil
}

// writeZip streams every entry into a single archive at bundle. Entries are
// stored under a top-level directory equal to the bundle name so unzipping
// yields one tidy folder.
func writeZip(bundle, name string, entries []Entry) error {
	f, err := os.Create(bundle)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, e := range entries {
		inZip := path.Join(name, e.Dest)
		w, err := zw.Create(inZip)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("zip entry %q: %w", inZip, err)
		}
		src, err := os.Open(e.Source)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("open %q: %w", e.Source, err)
		}
		if _, err := io.Copy(w, src); err != nil {
			_ = src.Close()
			_ = zw.Close()
			return fmt.Errorf("copy %q into zip: %w", e.Source, err)
		}
		_ = src.Close()
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize zip: %w", err)
	}
	return nil
}

// copyFile copies src to dst by content only (no move, no delete of src). The
// destination is created fresh; any prior content is replaced.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %q: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %q -> %q: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %q: %w", dst, err)
	}
	return nil
}
