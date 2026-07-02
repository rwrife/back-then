package sessions

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/store"
)

// base is a fixed reference instant so all synthetic timelines are stable.
var base = time.Date(2025, time.March, 10, 9, 0, 0, 0, time.UTC)

// rec builds a FileRecord with the given path, folder, extension, and an
// effective time expressed as minutes after base (via ModTime).
func rec(path, dir, ext string, offsetMin int) store.FileRecord {
	return store.FileRecord{
		Path:      path,
		ParentDir: dir,
		Ext:       ext,
		Size:      100,
		ModTime:   base.Add(time.Duration(offsetMin) * time.Minute),
	}
}

// TestClusterKnownBursts feeds a timeline with three deliberate bursts
// separated by large gaps and asserts they split into exactly three sessions.
func TestClusterKnownBursts(t *testing.T) {
	files := []store.FileRecord{
		// Burst A: three files within ~5 min.
		rec("/d/a1.txt", "/d", ".txt", 0),
		rec("/d/a2.txt", "/d", ".txt", 2),
		rec("/d/a3.txt", "/d", ".txt", 5),
		// ~6h later — clearly a new session.
		rec("/e/b1.jpg", "/e", ".jpg", 6*60),
		rec("/e/b2.jpg", "/e", ".jpg", 6*60+3),
		// ~another 5h later.
		rec("/f/c1.pdf", "/f", ".pdf", 11*60),
	}

	got := Cluster(files, Options{FolderAware: true})
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3", len(got))
	}
	if got[0].Count() != 3 || got[1].Count() != 2 || got[2].Count() != 1 {
		t.Fatalf("session counts = %d/%d/%d, want 3/2/1",
			got[0].Count(), got[1].Count(), got[2].Count())
	}
	// Chronological order: first session starts before the last.
	if !got[0].Start.Before(got[2].Start) {
		t.Errorf("sessions not in chronological order")
	}
	// Span of the first session is first->last file (0 to 5 min).
	if got[0].Duration() != 5*time.Minute {
		t.Errorf("first session duration = %v, want 5m", got[0].Duration())
	}
}

// TestClusterUnorderedInput verifies input order does not matter: shuffled
// records must yield the same sessions as sorted ones.
func TestClusterUnorderedInput(t *testing.T) {
	files := []store.FileRecord{
		rec("/d/c.txt", "/d", ".txt", 5),
		rec("/d/a.txt", "/d", ".txt", 0),
		rec("/e/z.txt", "/e", ".txt", 6*60),
		rec("/d/b.txt", "/d", ".txt", 2),
	}
	got := Cluster(files, Options{FolderAware: true})
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	if got[0].Count() != 3 || got[1].Count() != 1 {
		t.Fatalf("counts = %d/%d, want 3/1", got[0].Count(), got[1].Count())
	}
	// Members of the first session must be in timeline order.
	want := []string{"/d/a.txt", "/d/b.txt", "/d/c.txt"}
	for i, f := range got[0].Files {
		if f.Path != want[i] {
			t.Errorf("session[0] file %d = %s, want %s", i, f.Path, want[i])
		}
	}
}

// TestFolderAwareWidensGap checks that two files ~3h apart split when in
// different folders (base gap 2h) but stay together when in the same folder
// (folder-aware factor doubles the tolerance to 4h).
func TestFolderAwareWidensGap(t *testing.T) {
	gap := 2 * time.Hour

	diffFolders := []store.FileRecord{
		rec("/x/a.txt", "/x", ".txt", 0),
		rec("/y/b.txt", "/y", ".txt", 3*60), // 3h later, different folder
	}
	if got := Cluster(diffFolders, Options{Gap: gap, FolderAware: true}); len(got) != 2 {
		t.Errorf("different folders 3h apart: got %d sessions, want 2", len(got))
	}

	sameFolder := []store.FileRecord{
		rec("/x/a.txt", "/x", ".txt", 0),
		rec("/x/b.txt", "/x", ".txt", 3*60), // 3h later, same folder
	}
	if got := Cluster(sameFolder, Options{Gap: gap, FolderAware: true}); len(got) != 1 {
		t.Errorf("same folder 3h apart: got %d sessions, want 1", len(got))
	}
}

// TestTopFolderAndDominantExts exercises the per-session summaries.
func TestTopFolderAndDominantExts(t *testing.T) {
	s := Cluster([]store.FileRecord{
		rec("/photos/1.jpg", "/photos", ".jpg", 0),
		rec("/photos/2.jpg", "/photos", ".jpg", 1),
		rec("/photos/3.jpg", "/photos", ".jpg", 2),
		rec("/photos/clip.mov", "/photos", ".mov", 3),
		rec("/notes/x.txt", "/notes", ".txt", 4),
	}, Options{FolderAware: true})
	if len(s) != 1 {
		t.Fatalf("got %d sessions, want 1", len(s))
	}
	dir, n := s[0].TopFolder()
	if dir != "/photos" || n != 4 {
		t.Errorf("TopFolder = %q,%d want /photos,4", dir, n)
	}
	exts := s[0].DominantExts(2)
	if len(exts) != 2 || exts[0].Ext != ".jpg" || exts[0].Count != 3 {
		t.Errorf("DominantExts[0] = %+v, want .jpg×3", exts[0])
	}
}

// TestNearProximity checks that Near returns co-arriving files ordered by
// absolute proximity and excludes the target and out-of-window files.
func TestNearProximity(t *testing.T) {
	files := []store.FileRecord{
		rec("/d/target.txt", "/d", ".txt", 100),
		rec("/d/before.txt", "/d", ".txt", 90),   // 10m before
		rec("/d/after.txt", "/d", ".txt", 105),   // 5m after
		rec("/d/far.txt", "/d", ".txt", 100+600), // 10h after -> out of window
	}
	got := Near(files, "/d/target.txt", 6*time.Hour)
	if len(got) != 2 {
		t.Fatalf("got %d near results, want 2 (far one excluded)", len(got))
	}
	// Closest first: after.txt (+5m) before before.txt (-10m).
	if got[0].File.Path != "/d/after.txt" {
		t.Errorf("closest = %s, want /d/after.txt", got[0].File.Path)
	}
	if got[1].File.Path != "/d/before.txt" {
		t.Errorf("second = %s, want /d/before.txt", got[1].File.Path)
	}
	// Signed deltas.
	if got[0].Delta != 5*time.Minute {
		t.Errorf("after delta = %v, want +5m", got[0].Delta)
	}
	if got[1].Delta != -10*time.Minute {
		t.Errorf("before delta = %v, want -10m", got[1].Delta)
	}
}

// TestNearMissingTarget returns nil when the target path is absent.
func TestNearMissingTarget(t *testing.T) {
	files := []store.FileRecord{rec("/d/a.txt", "/d", ".txt", 0)}
	if got := Near(files, "/d/nope.txt", time.Hour); got != nil {
		t.Errorf("Near with missing target = %v, want nil", got)
	}
}

// TestEffectiveTimePrefersCapture verifies capture > create > mod ordering
// influences clustering.
func TestEffectiveTimePrefersCapture(t *testing.T) {
	r := store.FileRecord{
		Path:        "/d/x.jpg",
		ModTime:     base,
		CreateTime:  base.Add(-time.Hour),
		CaptureTime: base.Add(-48 * time.Hour),
	}
	if !r.EffectiveTime().Equal(base.Add(-48 * time.Hour)) {
		t.Errorf("EffectiveTime = %v, want capture time", r.EffectiveTime())
	}
}

// TestSameFolderAncestor treats parent/child directories as the same area.
func TestSameFolderAncestor(t *testing.T) {
	parent := filepath.Clean("/a/b")
	child := filepath.Clean("/a/b/c")
	if !sameFolder(parent, child) {
		t.Errorf("sameFolder(%q,%q) = false, want true", parent, child)
	}
	if sameFolder("/a/b", "/a/z") {
		t.Errorf("sameFolder of siblings = true, want false")
	}
}
