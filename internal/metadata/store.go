// Package metadata is the metadata service: it records information about
// backups (jobs, snapshots), not the backup data itself, as documented in
// docs/02-architecture.md. It is the system of record for job durability -
// see internal/queue's Queue contract: callers must persist a job here as
// queued before handing it to a Queue, so a dropped enqueue is recoverable.
//
// Two backends are supported: SQLite via modernc.org/sqlite (pure Go, no
// CGO, the Phase 1 single-controller default - see Open) and PostgreSQL via
// jackc/pgx (also pure Go, no CGO - keeps CGO_ENABLED=0 cross-compilation
// working, see .github/workflows/release.yml) - the documented prerequisite
// for multi-controller HA (docs/06-high-availability.md): every controller
// process needs to see the same jobs/snapshots/events tables, which a local
// SQLite file can't provide. See OpenPostgres.
package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

type Store struct {
	db      *sql.DB
	dialect dialect
}

const sqliteSchema = `
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

// postgresSchema is the same shape as sqliteSchema, adapted for Postgres's
// type system: TIMESTAMPTZ (a real timezone-aware timestamp type - unlike
// SQLite's TEXT-affinity DATETIME, which stores time.Time as a string and
// can hit the sub-second lexicographic-ordering trap events_datetime_check_
// test.go guards against; a native TIMESTAMPTZ column can't) and GENERATED
// ALWAYS AS IDENTITY instead of SQLite's AUTOINCREMENT.
const postgresSchema = `
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	status TEXT NOT NULL,
	retention_daily INTEGER NOT NULL DEFAULT 0,
	retention_weekly INTEGER NOT NULL DEFAULT 0,
	retention_monthly INTEGER NOT NULL DEFAULT 0,
	retention_yearly INTEGER NOT NULL DEFAULT 0,
	queued_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS snapshots (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	size BIGINT NOT NULL,
	checksum TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
	id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	type TEXT NOT NULL,
	job_id TEXT NOT NULL,
	host TEXT NOT NULL,
	plugin TEXT NOT NULL,
	resource TEXT NOT NULL,
	fields TEXT NOT NULL,
	timestamp TIMESTAMPTZ NOT NULL
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

	if _, err := db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("metadata: apply schema: %w", err)
	}

	return &Store{db: db, dialect: dialectSQLite}, nil
}

// OpenPostgres opens a metadata store backed by PostgreSQL instead of
// SQLite - see docs/06-high-availability.md, where this is the documented
// prerequisite for multi-controller HA. dsn is a standard Postgres
// connection string (e.g. "postgres://user:pass@host:5432/dbname").
//
// Unlike SQLite, Postgres is a real client-server database with its own
// concurrent-writer handling, so this does not pin the pool to a single
// connection the way Open does.
func OpenPostgres(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("metadata: open %s: %w", redactDSN(dsn), err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("metadata: connect to %s: %w", redactDSN(dsn), err)
	}

	if _, err := db.Exec(postgresSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("metadata: apply schema: %w", err)
	}

	return &Store{db: db, dialect: dialectPostgres}, nil
}

// redactDSN keeps a Postgres connection string's password out of error
// messages - dsn errors (typos, an unreachable host) are common enough that
// this is worth doing rather than risking a credential in a log line. Only
// handles the URL form (postgres://user:pass@host/db); a keyword/value DSN
// ("host=... password=...") falls back to a generic placeholder rather than
// risk missing a credential in a format this doesn't parse.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return "<postgres dsn>"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}

func (s *Store) Close() error {
	return s.db.Close()
}

// exec/query/queryRow rebind SQLite-style "?" placeholders to Postgres's
// "$1, $2, ..." when the store is backed by Postgres, so every method in
// this package (jobs.go, snapshots.go, events.go) can write one query
// literal instead of maintaining two dialects of SQL text. A no-op for
// SQLite. Every call site in this package goes through these instead of
// s.db directly, specifically so that guarantee holds everywhere.
func (s *Store) exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.rebind(query), args...)
}

func (s *Store) query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.rebind(query), args...)
}

func (s *Store) queryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return s.db.QueryRowContext(ctx, s.rebind(query), args...)
}

// rebind is a no-op for SQLite. None of this package's queries embed a
// literal "?" inside a string value, so a naive positional replacement is
// safe - if a future query needs to, this would need to get smarter about
// skipping quoted sections.
func (s *Store) rebind(query string) string {
	if s.dialect != dialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
