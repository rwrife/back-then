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

	"github.com/rwrife/back-then/internal/walk"
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
