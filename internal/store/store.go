// Package store is the SQLite-backed index: open/migrate the local database,
// upsert FileSignal records (incremental, skipping unchanged files), and run
// time-window queries that power find/sessions/near.
//
// Implemented in M2. Pure-Go SQLite (modernc.org/sqlite) keeps the build
// cgo-free and the index a single portable file.
package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rwrife/back-then/internal/rank"
	"github.com/rwrife/back-then/internal/walk"
	"github.com/rwrife/back-then/internal/when"
)

// schemaVersion is bumped when the on-disk schema changes in an incompatible
// way. It is stored in PRAGMA user_version.
const schemaVersion = 1

// schema is the DDL applied on open. It is idempotent (IF NOT EXISTS) so
// opening an existing index is a no-op beyond the version check.
const schema = `
CREATE TABLE IF NOT EXISTS files (
	path        TEXT    NOT NULL PRIMARY KEY,
	size        INTEGER NOT NULL,
	mod_time    INTEGER NOT NULL, -- unix nanoseconds
	create_time INTEGER NOT NULL, -- unix nanoseconds, 0 if unknown
	capture_time INTEGER NOT NULL, -- unix nanoseconds, 0 if unknown (M5)
	ext         TEXT    NOT NULL,
	parent_dir  TEXT    NOT NULL,
	indexed_at  INTEGER NOT NULL  -- unix nanoseconds of last upsert
);
CREATE INDEX IF NOT EXISTS idx_files_mod_time ON files(mod_time);
CREATE INDEX IF NOT EXISTS idx_files_ext      ON files(ext);
CREATE INDEX IF NOT EXISTS idx_files_parent   ON files(parent_dir);
`

// Store wraps the SQLite handle and the prepared statements back-then uses.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite index at path and applies the
// schema. The caller must Close the returned Store.
//
// A path of ":memory:" yields an ephemeral in-memory index, used by tests.
func Open(path string) (*Store, error) {
	// Busy timeout avoids spurious "database is locked" under brief contention;
	// foreign_keys is harmless here but good hygiene.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	if path == ":memory:" {
		dsn = path
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// modernc/sqlite is a single connection per handle for in-memory DBs;
	// limiting connections keeps WAL behavior predictable for a CLI.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if ver != 0 && ver != schemaVersion {
		return fmt.Errorf("index schema version %d is newer than supported %d; upgrade back-then", ver, schemaVersion)
	}
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if ver == 0 {
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return fmt.Errorf("set user_version: %w", err)
		}
	}
	return nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// IndexResult summarizes the outcome of an Index call.
type IndexResult struct {
	// Seen is the number of files visited during the walk.
	Seen int
	// Upserted is the number of files inserted or updated (changed size/mtime
	// or brand new).
	Upserted int
	// Skipped is the number of files left untouched because their size and
	// mod time were unchanged since the last index (the incremental fast path).
	Skipped int
}

// nanos converts a time to unix nanoseconds, mapping the zero time to 0 so
// "unknown" is stored as 0 rather than a large negative sentinel.
func nanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// Index walks each root and upserts every discovered file, skipping files
// whose size and mod time are unchanged since the previous index. It returns
// counts of seen/upserted/skipped files.
//
// The whole operation runs in a single transaction so an interrupted index
// leaves the database consistent (all-or-nothing).
func (s *Store) Index(roots []string, opts walk.Options) (IndexResult, error) {
	var res IndexResult

	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Existing (size, mod_time) per path, to decide skip vs upsert without a
	// round-trip per file.
	type fp struct {
		size    int64
		modTime int64
	}
	existing := map[string]fp{}
	rows, err := tx.Query("SELECT path, size, mod_time FROM files")
	if err != nil {
		return res, fmt.Errorf("load existing: %w", err)
	}
	for rows.Next() {
		var p string
		var f fp
		if err := rows.Scan(&p, &f.size, &f.modTime); err != nil {
			rows.Close()
			return res, fmt.Errorf("scan existing: %w", err)
		}
		existing[p] = f
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("iterate existing: %w", err)
	}
	rows.Close()

	upsert, err := tx.Prepare(`
INSERT INTO files (path, size, mod_time, create_time, capture_time, ext, parent_dir, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
	size=excluded.size,
	mod_time=excluded.mod_time,
	create_time=excluded.create_time,
	capture_time=excluded.capture_time,
	ext=excluded.ext,
	parent_dir=excluded.parent_dir,
	indexed_at=excluded.indexed_at
`)
	if err != nil {
		return res, fmt.Errorf("prepare upsert: %w", err)
	}
	defer upsert.Close()

	now := time.Now().UnixNano()

	walkErr := walk.Walk(roots, opts, func(sig walk.FileSignal) error {
		res.Seen++
		mt := nanos(sig.ModTime)
		if prev, ok := existing[sig.Path]; ok && prev.size == sig.Size && prev.modTime == mt {
			res.Skipped++
			return nil
		}
		if _, err := upsert.Exec(
			sig.Path,
			sig.Size,
			mt,
			nanos(sig.CreateTime),
			nanos(sig.CaptureTime),
			sig.Ext,
			sig.ParentDir,
			now,
		); err != nil {
			return fmt.Errorf("upsert %q: %w", sig.Path, err)
		}
		res.Upserted++
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// Candidates returns files whose effective timestamp falls within the query
// window w, padded by a margin on each side so that ranking's proximity decay
// has nearby-but-outside files to consider. The margin scales with the window
// width (bounded) so a broad query pulls a proportionally wider candidate set.
//
// The effective timestamp is the EXIF capture time when present, else the
// modified time (mirroring rank.Candidate.When). Results are ordered by that
// timestamp ascending; ranking re-sorts by score. limit caps the number of
// rows scanned from the store (<= 0 means a generous default).
func (s *Store) Candidates(w when.Window, limit int) ([]rank.Candidate, error) {
	if limit <= 0 {
		limit = 5000
	}
	margin := candidateMargin(w)
	lo := w.Start.Add(-margin)
	hi := w.End.Add(margin)

	// COALESCE(capture_time,0) is stored as 0 when unknown; treat 0 as "use
	// mod_time" via the effective-time expression below so both columns are
	// filtered and ordered consistently with rank.Candidate.When().
	const effective = "CASE WHEN capture_time != 0 THEN capture_time ELSE mod_time END"
	q := fmt.Sprintf(`
SELECT path, size, mod_time, capture_time, ext, parent_dir
FROM files
WHERE %[1]s >= ? AND %[1]s < ?
ORDER BY %[1]s ASC
LIMIT ?`, effective)

	rows, err := s.db.Query(q, lo.UnixNano(), hi.UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var out []rank.Candidate
	for rows.Next() {
		var (
			c       rank.Candidate
			mod     int64
			capture int64
		)
		if err := rows.Scan(&c.Path, &c.Size, &mod, &capture, &c.Ext, &c.ParentDir); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		if mod != 0 {
			c.ModTime = time.Unix(0, mod)
		}
		if capture != 0 {
			c.CaptureTime = time.Unix(0, capture)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidates: %w", err)
	}
	return out, nil
}

// candidateMargin is the padding added on each side of the query window when
// pulling candidates, equal to the window width (so the search span is ~3x the
// window) but clamped between a floor and a ceiling to keep result sets useful
// for both pinpoint and broad queries.
func candidateMargin(w when.Window) time.Duration {
	width := w.End.Sub(w.Start)
	margin := width
	const (
		floor = 3 * 24 * time.Hour
		ceil  = 365 * 24 * time.Hour
	)
	if margin < floor {
		margin = floor
	}
	if margin > ceil {
		margin = ceil
	}
	return margin
}

// FileRecord is a single indexed file as returned by AllFiles/FileByPath. It
// carries the fields sessions/near need: identity, size, extension, folder,
// and the timestamps used to place the file on the timeline.
type FileRecord struct {
	Path      string
	Size      int64
	Ext       string
	ParentDir string
	// ModTime is always present. CreateTime/CaptureTime are the zero value
	// when unknown (stored as 0).
	ModTime     time.Time
	CreateTime  time.Time
	CaptureTime time.Time
}

// EffectiveTime is the timestamp back-then uses to place a file on the
// timeline. Capture time (EXIF, when populated) is the most meaningful and
// always wins. Otherwise back-then uses the EARLIEST of creation/birth time
// and modification time.
//
// Why earliest, not "creation preferred"? back-then answers "when did this
// file land in my life?". Two real cases pull in opposite directions:
//   - A file edited long after it arrived has a fresh mtime; its (older)
//     birth time is the truer arrival signal.
//   - A file copied/extracted/restored recently often keeps its original
//     (older) mtime while getting a fresh creation time (this is exactly how
//     Windows behaves — CreationTime is set to "now" on copy). Here the older
//     mtime is the truer signal.
//
// Taking the earliest of the two honors whichever is older and avoids letting
// a copy's "now" creation time bury an old file's real timestamp. It also
// makes clustering deterministic across platforms: Linux (no birth time) and
// Windows (birth time = copy time) now agree whenever mtime is the older,
// meaningful value. Capture time still overrides both because EXIF is the
// authoritative content date.
func (f FileRecord) EffectiveTime() time.Time {
	if !f.CaptureTime.IsZero() {
		return f.CaptureTime
	}
	if !f.CreateTime.IsZero() && f.CreateTime.Before(f.ModTime) {
		return f.CreateTime
	}
	return f.ModTime
}

// timeOrZero maps a stored unix-nanosecond value back to a time, treating the
// 0 sentinel as "unknown" (the zero time).
func timeOrZero(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// scanFiles reads FileRecords from an open *sql.Rows selecting the standard
// column set (path,size,ext,parent_dir,mod_time,create_time,capture_time).
func scanFiles(rows *sql.Rows) ([]FileRecord, error) {
	var out []FileRecord
	for rows.Next() {
		var r FileRecord
		var mod, create, capture int64
		if err := rows.Scan(&r.Path, &r.Size, &r.Ext, &r.ParentDir, &mod, &create, &capture); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		r.ModTime = timeOrZero(mod)
		r.CreateTime = timeOrZero(create)
		r.CaptureTime = timeOrZero(capture)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	return out, nil
}

// effectiveTimeSQL is the SQL expression mirroring FileRecord.EffectiveTime:
// capture time when known, otherwise the EARLIEST of creation and mod time
// (mod alone when no creation time). It must stay in lock-step with the Go
// method so on-disk ordering and in-memory placement agree on every platform.
//
// SQLite's scalar min() propagates NULL, so MIN(NULLIF(create_time,0),mod_time)
// yields NULL when create_time is 0/unknown, letting the outer COALESCE fall
// back to mod_time.
const effectiveTimeSQL = `COALESCE(NULLIF(capture_time,0), MIN(NULLIF(create_time,0), mod_time), mod_time)`

// AllFiles returns every indexed file. Records are ordered by effective time
// (see effectiveTimeSQL) ascending, then path, so callers that cluster along
// the timeline can consume them in order without re-sorting.
func (s *Store) AllFiles() ([]FileRecord, error) {
	rows, err := s.db.Query(`
SELECT path, size, ext, parent_dir, mod_time, create_time, capture_time
FROM files
ORDER BY ` + effectiveTimeSQL + ` ASC, path ASC`)
	if err != nil {
		return nil, fmt.Errorf("query all files: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// FileByPath returns the record for an exact indexed path. The boolean is
// false (with a nil error) when no such path is indexed.
func (s *Store) FileByPath(path string) (FileRecord, bool, error) {
	rows, err := s.db.Query(`
SELECT path, size, ext, parent_dir, mod_time, create_time, capture_time
FROM files WHERE path = ?`, path)
	if err != nil {
		return FileRecord{}, false, fmt.Errorf("query file: %w", err)
	}
	defer rows.Close()
	recs, err := scanFiles(rows)
	if err != nil {
		return FileRecord{}, false, err
	}
	if len(recs) == 0 {
		return FileRecord{}, false, nil
	}
	return recs[0], true, nil
}

// windowEffectiveSQL is the effective-time expression used by window filters
// that operate row-by-row (Candidates, Forget). It mirrors
// FileRecord.EffectiveTime for the common case: capture time wins, else the
// earliest of creation and mod time, else mod time.
//
// It differs from effectiveTimeSQL only in spelling (a CASE ladder vs nested
// COALESCE/MIN); both compute the same value. Forget reuses this so the set it
// deletes is exactly the set find/near would have placed inside the window.
const windowEffectiveSQL = `CASE
	WHEN capture_time != 0 THEN capture_time
	WHEN create_time != 0 AND create_time < mod_time THEN create_time
	ELSE mod_time
END`

// Forget deletes every indexed file whose effective timestamp falls within the
// half-open window [w.Start, w.End) and returns the number of rows removed.
//
// Unlike Candidates, Forget applies no margin: it prunes exactly the window the
// caller resolved, so "forget last spring" removes precisely the files find
// would have anchored to that span. The delete runs in a single transaction.
//
// This is the destructive, opt-in privacy/space command from M6. It only
// touches the index; it never deletes files from disk.
func (s *Store) Forget(w when.Window) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM files WHERE `+windowEffectiveSQL+` >= ? AND `+windowEffectiveSQL+` < ?`,
		w.Start.UnixNano(), w.End.UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("forget window: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("forget rows affected: %w", err)
	}
	return n, nil
}

// CountInWindow reports how many indexed files have an effective timestamp
// inside the half-open window [w.Start, w.End). It is the read-only companion
// to Forget, powering `back-then forget --dry-run` so a stranger can preview
// exactly what a real forget would prune before committing to it.
func (s *Store) CountInWindow(w when.Window) (int64, error) {
	var n int64
	row := s.db.QueryRow(
		`SELECT COUNT(*) FROM files WHERE `+windowEffectiveSQL+` >= ? AND `+windowEffectiveSQL+` < ?`,
		w.Start.UnixNano(), w.End.UnixNano(),
	)
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count window: %w", err)
	}
	return n, nil
}

// ExtCount is a single extension's file count, used in Stats.TopExts.
type ExtCount struct {
	Ext   string
	Count int
}

// Stats is a summary of the current index contents.
type Stats struct {
	// Files is the total number of indexed files.
	Files int
	// TotalSize is the sum of all indexed file sizes in bytes.
	TotalSize int64
	// Oldest / Newest are the min/max mod times across the index. They are the
	// zero value when the index is empty.
	Oldest time.Time
	Newest time.Time
	// TopExts lists the most common extensions, most-frequent first.
	TopExts []ExtCount
}

// Stats computes a summary of the index. topN caps how many extensions appear
// in TopExts (<= 0 means a sensible default of 10).
func (s *Store) Stats(topN int) (Stats, error) {
	if topN <= 0 {
		topN = 10
	}
	var st Stats

	var count int
	var totalSize sql.NullInt64
	var minMod, maxMod sql.NullInt64
	row := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size),0), MIN(mod_time), MAX(mod_time) FROM files`)
	if err := row.Scan(&count, &totalSize, &minMod, &maxMod); err != nil {
		return st, fmt.Errorf("stats summary: %w", err)
	}
	st.Files = count
	st.TotalSize = totalSize.Int64
	if minMod.Valid && minMod.Int64 != 0 {
		st.Oldest = time.Unix(0, minMod.Int64)
	}
	if maxMod.Valid && maxMod.Int64 != 0 {
		st.Newest = time.Unix(0, maxMod.Int64)
	}

	rows, err := s.db.Query(`
SELECT CASE WHEN ext = '' THEN '(none)' ELSE ext END AS e, COUNT(*) AS c
FROM files GROUP BY e ORDER BY c DESC, e ASC LIMIT ?`, topN)
	if err != nil {
		return st, fmt.Errorf("stats exts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ec ExtCount
		if err := rows.Scan(&ec.Ext, &ec.Count); err != nil {
			return st, fmt.Errorf("scan ext: %w", err)
		}
		st.TopExts = append(st.TopExts, ec)
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("iterate exts: %w", err)
	}
	// Defensive: ensure deterministic order even if the driver reorders ties.
	sort.SliceStable(st.TopExts, func(i, j int) bool {
		if st.TopExts[i].Count != st.TopExts[j].Count {
			return st.TopExts[i].Count > st.TopExts[j].Count
		}
		return st.TopExts[i].Ext < st.TopExts[j].Ext
	})

	return st, nil
}
