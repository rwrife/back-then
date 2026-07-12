package timeline

import (
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
)

// mkSession builds a session spanning [start, start+dur] with a single file so
// TopFolder/exts have something to report.
func mkSession(start time.Time, dur time.Duration, dir, ext string) sessions.Session {
	f := store.FileRecord{
		Path:      dir + "/f" + ext,
		Ext:       ext,
		ParentDir: dir,
		ModTime:   start,
	}
	return sessions.Session{Start: start, End: start.Add(dur), Files: []store.FileRecord{f}}
}

func TestNewEmpty(t *testing.T) {
	m := New(nil, 10)
	if m.Width() != 10 {
		t.Fatalf("width = %d, want 10", m.Width())
	}
	if got := len(m.CurrentSessions()); got != 0 {
		t.Fatalf("empty model has %d current sessions, want 0", got)
	}
	if m.MaxHeat() != 0 {
		t.Fatalf("empty MaxHeat = %d, want 0", m.MaxHeat())
	}
}

func TestNewClampsWidth(t *testing.T) {
	m := New(nil, 0)
	if m.Width() != 1 {
		t.Fatalf("width = %d, want clamp to 1", m.Width())
	}
}

func TestBucketingSpreadsSessions(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sess := []sessions.Session{
		mkSession(base, time.Hour, "/a", ".txt"),                   // start of span
		mkSession(base.Add(24*time.Hour), time.Hour, "/b", ".jpg"), // middle-ish
		mkSession(base.Add(48*time.Hour), time.Hour, "/c", ".pdf"), // end of span
	}
	m := New(sess, 3)

	// First and last columns must hold the boundary sessions.
	if m.Columns[0].Heat() == 0 {
		t.Errorf("first column should hold the earliest session")
	}
	if m.Columns[len(m.Columns)-1].Heat() == 0 {
		t.Errorf("last column should hold the latest session")
	}

	// Every session must land in exactly one column.
	total := 0
	for _, c := range m.Columns {
		total += c.Heat()
	}
	if total != len(sess) {
		t.Errorf("bucketed %d sessions, want %d", total, len(sess))
	}
}

func TestCursorStartsOnNewestNonEmpty(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sess := []sessions.Session{
		mkSession(base, time.Hour, "/a", ".txt"),
		mkSession(base.Add(10*time.Hour), time.Hour, "/b", ".jpg"),
	}
	// Wide axis so the newest session sits well before the final (empty) column
	// only if span extends; here span ends at newest, so it lands in last col.
	m := New(sess, 20)
	cur := m.CurrentSessions()
	if len(cur) == 0 {
		t.Fatalf("cursor should start on a populated column")
	}
	if !cur[0].Start.Equal(base.Add(10 * time.Hour)) {
		t.Errorf("cursor session start = %v, want newest %v", cur[0].Start, base.Add(10*time.Hour))
	}
}

func TestLeftRightHomeEnd(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sess := []sessions.Session{mkSession(base, time.Hour, "/a", ".txt")}
	m := New(sess, 5)

	m.JumpEnd()
	if m.Cursor != 4 {
		t.Fatalf("End cursor = %d, want 4", m.Cursor)
	}
	if m.Right() {
		t.Errorf("Right at end should not move")
	}
	m.Home()
	if m.Cursor != 0 {
		t.Fatalf("Home cursor = %d, want 0", m.Cursor)
	}
	if m.Left() {
		t.Errorf("Left at home should not move")
	}
	if !m.Right() || m.Cursor != 1 {
		t.Errorf("Right should move to 1, cursor=%d", m.Cursor)
	}
	if !m.Left() || m.Cursor != 0 {
		t.Errorf("Left should move back to 0, cursor=%d", m.Cursor)
	}
}

func TestPrevNextSessionSkipsGaps(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two sessions far apart so empty columns sit between them.
	sess := []sessions.Session{
		mkSession(base, time.Hour, "/a", ".txt"),
		mkSession(base.Add(100*time.Hour), time.Hour, "/b", ".jpg"),
	}
	m := New(sess, 20)
	m.Home() // oldest, populated

	if !m.NextSession() {
		t.Fatalf("NextSession should jump to the later populated column")
	}
	cur := m.CurrentSessions()
	if len(cur) == 0 || !cur[0].Start.Equal(base.Add(100*time.Hour)) {
		t.Errorf("NextSession landed on wrong column: %+v", cur)
	}
	if m.NextSession() {
		t.Errorf("NextSession past last populated column should not move")
	}
	if !m.PrevSession() {
		t.Fatalf("PrevSession should jump back to the earlier populated column")
	}
	cur = m.CurrentSessions()
	if len(cur) == 0 || !cur[0].Start.Equal(base) {
		t.Errorf("PrevSession landed on wrong column: %+v", cur)
	}
}

func TestSingleInstantSpan(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// All sessions at the exact same instant with zero duration.
	sess := []sessions.Session{
		mkSession(base, 0, "/a", ".txt"),
		mkSession(base, 0, "/b", ".jpg"),
	}
	m := New(sess, 8)
	total := 0
	for _, c := range m.Columns {
		total += c.Heat()
	}
	if total != len(sess) {
		t.Errorf("single-instant span bucketed %d, want %d", total, len(sess))
	}
	if len(m.CurrentSessions()) == 0 {
		t.Errorf("cursor should point at the populated column")
	}
}

func TestUnsortedInputIsOrdered(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sess := []sessions.Session{
		mkSession(base.Add(48*time.Hour), time.Hour, "/c", ".pdf"),
		mkSession(base, time.Hour, "/a", ".txt"),
		mkSession(base.Add(24*time.Hour), time.Hour, "/b", ".jpg"),
	}
	m := New(sess, 3)
	for i := 1; i < len(m.Sessions); i++ {
		if m.Sessions[i].Start.Before(m.Sessions[i-1].Start) {
			t.Fatalf("sessions not chronological at %d", i)
		}
	}
}
