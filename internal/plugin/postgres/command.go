package postgres

import (
	"strings"

	"bop/internal/plugin/shellcmd"
)

// execPrefix builds the leading argv tokens common to every command this
// plugin runs: either a plain `env PGPASSWORD=...` (direct on the host) or
// a `docker exec [-i] -e PGPASSWORD=...` into cfg.Container - most Postgres
// instances in this deployment are containerized. interactive requests the
// docker exec -i flag, needed only when a command has stdin piped to it
// (restoreCommand); psql -c and pg_dump don't.
func execPrefix(cfg postgresConfig, interactive bool) []string {
	if cfg.Container == "" {
		return []string{"env", "PGPASSWORD=" + cfg.Password}
	}
	args := []string{"docker", "exec"}
	if interactive {
		args = append(args, "-i")
	}
	return append(args, "-e", "PGPASSWORD="+cfg.Password, cfg.Container)
}

// dumpCommand builds the remote command to dump database via pg_dump.
// pg_dump's default plain-text SQL output format is used deliberately, so
// restore only ever needs psql, not pg_restore.
func dumpCommand(cfg postgresConfig, database string) string {
	args := append(execPrefix(cfg, false), "pg_dump", "-U", cfg.Username, database)
	return shellcmd.Build(args)
}

// restoreCommand builds the remote command to restore database from a
// plain-text SQL dump piped in on stdin. docker exec needs -i to keep
// stdin open when a container is involved.
func restoreCommand(cfg postgresConfig, database string) string {
	args := append(execPrefix(cfg, true), "psql", "-U", cfg.Username, database)
	return shellcmd.Build(args)
}

// createDatabaseCommand and dropDatabaseCommand build the remote commands
// that provision/tear down a restore-test's scratch database (see
// plugin.RestoreTestSuffix). Both connect to the "postgres" maintenance
// database - the database being created doesn't exist yet, and the one
// being dropped must not be the connection's own database - which every
// Postgres server has. -v ON_ERROR_STOP=1 makes psql exit non-zero (and
// print the SQL error to stderr) on failure instead of continuing past it.
func createDatabaseCommand(cfg postgresConfig, database string) string {
	args := append(execPrefix(cfg, false), "psql", "-U", cfg.Username, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", createDatabaseSQL(database))
	return shellcmd.Build(args)
}

func dropDatabaseCommand(cfg postgresConfig, database string) string {
	args := append(execPrefix(cfg, false), "psql", "-U", cfg.Username, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", dropDatabaseSQL(database))
	return shellcmd.Build(args)
}

func createDatabaseSQL(database string) string {
	return `CREATE DATABASE "` + escapeIdentifier(database) + `"`
}

func dropDatabaseSQL(database string) string {
	return `DROP DATABASE "` + escapeIdentifier(database) + `"`
}

// escapeIdentifier doubles embedded double quotes, the standard SQL way to
// escape a quoted identifier (database names are operator-controlled
// inventory data - see resource IDs in inventory.yaml - not free-form user
// input, but this is cheap and correct regardless, same reasoning as
// shellcmd.Quote).
func escapeIdentifier(s string) string {
	return strings.ReplaceAll(s, `"`, `""`)
}
