// Package timeline builds the data model behind `back-then timeline`, the
// interactive TUI scrubber. It is deliberately split from the bubbletea view so
// the axis math (bucketing sessions along a time axis, mapping a cursor column
// to a session) is pure and unit-testable without a terminal.
//
// The model turns a chronological list of sessions into a fixed number of
// evenly spaced "columns" along a horizontal time axis. Each column covers an
// equal slice of the [firstSession.Start, lastSession.End] span and knows how
// many sessions fall inside it (its "heat"). Scrubbing the cursor left/right
// moves between columns; the currently highlighted column resolves to the
// sessions that live there so the view can light them up and list their files.
package timeline

import (
	"time"

	"github.com/rwrife/back-then/internal/sessions"
)

// Column is one cell along the time axis. It covers the half-open time range
// [Start, End) and holds the indexes (into the Model's Sessions slice) of the
// sessions whose Start falls within that range.
type Column struct {
	Start   time.Time
	End     time.Time
	Session []int
}

// Heat is the number of sessions bucketed into the column. It drives how
// brightly the column renders on the axis.
func (c Column) Heat() int { return len(c.Session) }

// Model is the pure, view-agnostic state of the timeline scrubber. Construct it
// with New; drive the cursor with Left/Right/Home/End; ask CurrentSessions for
// what the cursor is pointing at.
type Model struct {
	// Sessions are the source sessions in chronological order (oldest first).
	Sessions []sessions.Session
	// Columns are the axis cells, oldest (index 0) to newest.
	Columns []Column
	// Cursor is the index of the highlighted column.
	Cursor int
	// Span brackets the whole timeline: Start of the first session to End of
	// the last. Zero span (empty or single-instant) is handled gracefully.
	Start time.Time
	End   time.Time
}

// New builds a Model that lays the given sessions out across width columns.
// Sessions may be passed in any order; they are sorted chronologically by their
// Start time. width must be >= 1; values below 1 are clamped to 1.
//
// The cursor starts on the newest non-empty column (or the last column when all
// are empty), because the most recent episode is usually what you are hunting.
func New(sess []sessions.Session, width int) Model {
	if width < 1 {
		width = 1
	}

	ordered := make([]sessions.Session, len(sess))
	copy(ordered, sess)
	sortByStart(ordered)

	m := Model{Sessions: ordered}
	if len(ordered) == 0 {
		m.Columns = make([]Column, width)
		return m
	}

	m.Start = ordered[0].Start
	m.End = ordered[len(ordered)-1].End
	if !m.End.After(m.Start) {
		// All sessions collapse to (near) a single instant: give the span a
		// tiny non-zero width so bucketing stays well-defined.
		m.End = m.Start.Add(time.Nanosecond)
	}

	total := m.End.Sub(m.Start)
	step := total / time.Duration(width)
	if step <= 0 {
		step = time.Nanosecond
	}

	cols := make([]Column, width)
	for i := range cols {
		cols[i].Start = m.Start.Add(step * time.Duration(i))
		if i == width-1 {
			cols[i].End = m.End
		} else {
			cols[i].End = m.Start.Add(step * time.Duration(i+1))
		}
	}

	for idx, s := range ordered {
		col := columnFor(m.Start, step, width, s.Start)
		cols[col].Session = append(cols[col].Session, idx)
	}

	m.Columns = cols
	m.Cursor = lastNonEmpty(cols)
	return m
}

// columnFor maps a time to its column index, clamped to [0, width-1].
func columnFor(start time.Time, step time.Duration, width int, t time.Time) int {
	if !t.After(start) {
		return 0
	}
	idx := int(t.Sub(start) / step)
	if idx < 0 {
		idx = 0
	}
	if idx >= width {
		idx = width - 1
	}
	return idx
}

// lastNonEmpty returns the index of the newest column that holds a session, or
// the final column index when every column is empty.
func lastNonEmpty(cols []Column) int {
	for i := len(cols) - 1; i >= 0; i-- {
		if cols[i].Heat() > 0 {
			return i
		}
	}
	if len(cols) == 0 {
		return 0
	}
	return len(cols) - 1
}

// Width is the number of columns on the axis.
func (m *Model) Width() int { return len(m.Columns) }

// Left moves the cursor one column toward the past, stopping at the first
// column. It reports whether the cursor actually moved.
func (m *Model) Left() bool {
	if m.Cursor > 0 {
		m.Cursor--
		return true
	}
	return false
}

// Right moves the cursor one column toward the present, stopping at the last
// column. It reports whether the cursor actually moved.
func (m *Model) Right() bool {
	if m.Cursor < len(m.Columns)-1 {
		m.Cursor++
		return true
	}
	return false
}

// Home jumps the cursor to the oldest column.
func (m *Model) Home() { m.Cursor = 0 }

// JumpEnd jumps the cursor to the newest column.
func (m *Model) JumpEnd() {
	if len(m.Columns) > 0 {
		m.Cursor = len(m.Columns) - 1
	}
}

// PrevSession jumps the cursor to the nearest non-empty column strictly before
// the current one, so arrow-through-sessions skips empty gaps. It reports
// whether it moved.
func (m *Model) PrevSession() bool {
	for i := m.Cursor - 1; i >= 0; i-- {
		if m.Columns[i].Heat() > 0 {
			m.Cursor = i
			return true
		}
	}
	return false
}

// NextSession jumps the cursor to the nearest non-empty column strictly after
// the current one. It reports whether it moved.
func (m *Model) NextSession() bool {
	for i := m.Cursor + 1; i < len(m.Columns); i++ {
		if m.Columns[i].Heat() > 0 {
			m.Cursor = i
			return true
		}
	}
	return false
}

// CurrentColumn returns the column under the cursor. It returns the zero Column
// when the model has no columns.
func (m *Model) CurrentColumn() Column {
	if m.Cursor < 0 || m.Cursor >= len(m.Columns) {
		return Column{}
	}
	return m.Columns[m.Cursor]
}

// CurrentSessions returns the sessions bucketed into the cursor's column, in
// chronological order. The slice is empty when the column holds none.
func (m *Model) CurrentSessions() []sessions.Session {
	col := m.CurrentColumn()
	out := make([]sessions.Session, 0, len(col.Session))
	for _, idx := range col.Session {
		if idx >= 0 && idx < len(m.Sessions) {
			out = append(out, m.Sessions[idx])
		}
	}
	return out
}

// MaxHeat is the highest column heat in the model, used to scale the axis
// rendering. It is 0 for an empty timeline.
func (m *Model) MaxHeat() int {
	max := 0
	for _, c := range m.Columns {
		if c.Heat() > max {
			max = c.Heat()
		}
	}
	return max
}

// sortByStart orders sessions chronologically by Start, ties broken by End,
// using a simple insertion sort to avoid pulling in extra imports for what are
// typically small, already-mostly-sorted slices.
func sortByStart(s []sessions.Session) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func less(a, b sessions.Session) bool {
	if !a.Start.Equal(b.Start) {
		return a.Start.Before(b.Start)
	}
	return a.End.Before(b.End)
}
