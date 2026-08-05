// Package metadata is the metadata service: it records information about
// backups (jobs, snapshots), not the backup data itself, as documented in
// docs/02-architecture.md. It is the system of record for job durability -
// see internal/queue's Queue contract: callers must persist a job here as
// queued before handing it to a Queue, so a dropped enqueue is recoverable.
//
// Backed by SQLite via modernc.org/sqlite (pure Go, no CGO) rather than
// mattn/go-sqlite3, since a CGO-based driver requires a C compiler that
// isn't guaranteed to be present on a bare Go install.
package metadata

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	status TEXT NOT NULL,
	retention_daily INTEGER NOT NULL DEFAULT 0,
	retention_weekly INTEGER NOT NULL DEFAULT 0,
	retention_monthly INTEGER NOT NULL DEFAULT 0,
	retention_yearly INTEGER NOT NULL DEFAULT 0,
	queued_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS snapshots (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	size INTEGER NOT NULL,
	checksum TEXT NOT NULL,
	created_at DATETIME NOT NULL
);

-- Unlike jobs/snapshots (one row per job), events fire many times per job
-- (discovery started, artifact created, upload started/completed, ...), so
-- this table is pruned by age - see PruneEventsOlderThan and
-- metadata.event_retention - rather than left to grow unbounded.
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	job_id TEXT NOT NULL,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	resource TEXT NOT NULL,
	fields TEXT NOT NULL,
	timestamp DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
`

// Open opens (creating if necessary) the SQLite database at dsn and applies
// the schema. dsn is a filesystem path, or ":memory:" for tests.
func Open(dsn string) (*Store, error) {
	if dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("metadata: create dir %s: %w", dir, err)
			}
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("metadata: open %s: %w", dsn, err)
	}

	// SQLite serializes writers regardless; a single pooled connection
	// avoids "database is locked" errors and, for ":memory:" DSNs, avoids
	// each pooled connection silently getting its own separate database.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("metadata: apply schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
