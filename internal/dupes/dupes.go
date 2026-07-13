// Package dupes surfaces likely-duplicate files by reusing back-then's
// time-clustering primitive instead of doing a full-disk hash sweep. Humans
// download or copy the same thing twice all the time; within a session (or an
// ad-hoc time window) two files that share an exact size and extension and
// arrived close together are very probably the same bytes.
//
// Grouping is cheap and metadata-only: same size + same extension within a
// configurable time gap. When exactness matters, callers hash the small
// candidate set (see Verify) so reported duplicates are confirmed byte-for-byte
// rather than merely suspected.
package dupes

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sort"

	"github.com/rwrife/back-then/internal/store"
)

// DefaultGap is the maximum time between two same-size/same-ext files for them
// to be treated as belonging to the same duplicate candidate cluster. A
// generous default: copies of the same file are usually created seconds apart,
// but a re-download days later still counts as a dupe, so we key primarily on
// identical size+ext and use the gap only to avoid conflating unrelated files
// of a coincidentally identical size across a whole index. Zero means "no gap
// limit" (group purely by size+ext).
const DefaultGap = 0

// Group is a set of files that are suspected (or, after Verify, confirmed)
// duplicates of one another. Files are ordered oldest-first, so Files[0] is the
// natural "keeper" (the original) and the rest are redundant copies.
type Group struct {
	// Size is the shared byte size of every file in the group.
	Size int64
	// Ext is the shared extension (including the dot, or "" for none).
	Ext string
	// Hash is the hex sha256 shared by the group, set only when the group was
	// produced with verification. Empty for metadata-only groups.
	Hash string
	// Files are the members, oldest-first by effective time.
	Files []store.FileRecord
}

// Count is the number of files in the group.
func (g Group) Count() int { return len(g.Files) }

// Wasted is the number of bytes that could be reclaimed by keeping one copy
// and removing the rest: Size * (Count-1).
func (g Group) Wasted() int64 {
	if len(g.Files) < 2 {
		return 0
	}
	return g.Size * int64(len(g.Files)-1)
}

// Dupes are the redundant copies (everything after the keeper).
func (g Group) Dupes() []store.FileRecord {
	if len(g.Files) < 2 {
		return nil
	}
	return g.Files[1:]
}

// Options tunes duplicate detection.
type Options struct {
	// Gap, when > 0, caps how far apart (by effective time) two files may be
	// and still land in the same candidate cluster. Zero uses no gap limit.
	Gap int64 // nanoseconds; matches time.Duration when cast

	// MinSize skips files smaller than this many bytes. Tiny files (empty
	// markers, lockfiles) collide on size constantly and rarely matter for
	// reclaiming space. Zero includes everything except zero-byte files.
	MinSize int64
}

// Find groups the given files into suspected duplicate groups using metadata
// only (identical size + extension, clustered by time gap when Options.Gap is
// set). Groups are returned largest-wasted-first so the biggest wins surface at
// the top. Zero-byte files are always skipped: many unrelated empty files share
// size 0 and grouping them is noise, never a space win.
func Find(files []store.FileRecord, opts Options) []Group {
	// Bucket by (size, ext); only buckets with 2+ members can yield dupes.
	type key struct {
		size int64
		ext  string
	}
	buckets := map[key][]store.FileRecord{}
	for _, f := range files {
		if f.Size <= 0 {
			continue
		}
		if opts.MinSize > 0 && f.Size < opts.MinSize {
			continue
		}
		k := key{size: f.Size, ext: f.Ext}
		buckets[k] = append(buckets[k], f)
	}

	var groups []Group
	for k, members := range buckets {
		if len(members) < 2 {
			continue
		}
		// Sort by effective time so gap-clustering and keeper selection are
		// deterministic (oldest first).
		sort.Slice(members, func(i, j int) bool {
			return members[i].EffectiveTime().Before(members[j].EffectiveTime())
		})

		for _, cluster := range clusterByGap(members, opts.Gap) {
			if len(cluster) < 2 {
				continue
			}
			groups = append(groups, Group{
				Size:  k.size,
				Ext:   k.ext,
				Files: cluster,
			})
		}
	}

	sortGroups(groups)
	return groups
}

// clusterByGap splits a time-sorted slice into sub-slices wherever consecutive
// files are more than gap apart. A gap of 0 (or negative) means one cluster.
func clusterByGap(sorted []store.FileRecord, gap int64) [][]store.FileRecord {
	if gap <= 0 || len(sorted) < 2 {
		return [][]store.FileRecord{sorted}
	}
	var out [][]store.FileRecord
	cur := []store.FileRecord{sorted[0]}
	prev := sorted[0].EffectiveTime()
	for _, f := range sorted[1:] {
		t := f.EffectiveTime()
		if t.Sub(prev).Nanoseconds() > gap {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, f)
		prev = t
	}
	out = append(out, cur)
	return out
}

// Verify confirms each metadata group by streaming a sha256 of every member and
// splitting the group by content hash. Files that fail to open (moved, perms)
// are dropped. Only sub-groups that still have 2+ identical-hash members are
// returned, so a Verify pass can only shrink the reported dupes — never invent
// them. Each returned group carries its shared Hash.
func Verify(groups []Group) ([]Group, error) {
	var out []Group
	for _, g := range groups {
		byHash := map[string][]store.FileRecord{}
		for _, f := range g.Files {
			h, err := hashFile(f.Path)
			if err != nil {
				// A candidate we can't read can't be confirmed; skip it rather
				// than fail the whole run (files move between index and verify).
				continue
			}
			byHash[h] = append(byHash[h], f)
		}
		for h, members := range byHash {
			if len(members) < 2 {
				continue
			}
			out = append(out, Group{
				Size:  g.Size,
				Ext:   g.Ext,
				Hash:  h,
				Files: members,
			})
		}
	}
	sortGroups(out)
	return out, nil
}

// hashFile streams a file through sha256 without loading it fully into memory.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TotalWasted sums the reclaimable bytes across all groups.
func TotalWasted(groups []Group) int64 {
	var total int64
	for _, g := range groups {
		total += g.Wasted()
	}
	return total
}

// sortGroups orders groups by wasted bytes descending, then by size, then by a
// stable path tiebreak so output is deterministic across runs.
func sortGroups(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		wi, wj := groups[i].Wasted(), groups[j].Wasted()
		if wi != wj {
			return wi > wj
		}
		if groups[i].Size != groups[j].Size {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].Files[0].Path < groups[j].Files[0].Path
	})
}
