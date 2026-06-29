// Package store is the SQLite-backed index: open/migrate the local database,
// upsert FileSignal records (incremental, skipping unchanged files), and run
// time-window queries that power find/sessions/near.
//
// Implemented in M2. Pure-Go SQLite (modernc.org/sqlite) keeps the build
// cgo-free and the index a single portable file.
package store
