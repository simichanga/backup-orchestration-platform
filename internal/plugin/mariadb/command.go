package mariadb

import (
	"strings"

	"bop/internal/plugin/shellcmd"
)

// execPrefix builds the leading argv tokens common to every command this
// plugin runs, with the password delivered via MYSQL_PWD (which both
// mariadb-dump and the mariadb client read) rather than a -p argument.
// interactive requests docker exec's -i flag, needed only when a command
// has stdin piped to it (restoreCommand).
//
// With PasswordContainerEnv, the command runs under
// `sh -c 'MYSQL_PWD="$VAR" exec "$@"' sh <argv...>` inside the container:
// the variable name is the only thing interpolated into the script
// (validated in ParseConfig), the real argv is passed through "$@"
// untouched, and the password itself never leaves the container.
func execPrefix(cfg Config, interactive bool) []string {
	if cfg.Container == "" {
		return []string{"env", "MYSQL_PWD=" + cfg.Password}
	}
	args := []string{"docker", "exec"}
	if interactive {
		args = append(args, "-i")
	}
	if cfg.PasswordContainerEnv != "" {
		return append(args, cfg.Container, "sh", "-c", `MYSQL_PWD="$`+cfg.PasswordContainerEnv+`" exec "$@"`, "sh")
	}
	return append(args, "-e", "MYSQL_PWD="+cfg.Password, cfg.Container)
}

// dumpCommand builds the remote command to dump database via
// mariadb-dump. --single-transaction gives a consistent snapshot of
// InnoDB tables without locking them; routines/triggers/events are
// included so a restore is complete. The database is passed positionally
// rather than via --databases on purpose: that omits CREATE DATABASE/USE
// from the dump, so it can be restored into a differently-named database
// (the restore-test scratch target).
func dumpCommand(cfg Config, database string) string {
	args := append(execPrefix(cfg, false), "mariadb-dump", "-u", cfg.Username,
		"--single-transaction", "--routines", "--triggers", "--events", "--hex-blob", database)
	return shellcmd.Build(args)
}

// restoreCommand builds the remote command to restore database from a SQL
// dump piped in on stdin.
func restoreCommand(cfg Config, database string) string {
	args := append(execPrefix(cfg, true), "mariadb", "-u", cfg.Username, database)
	return shellcmd.Build(args)
}

// createDatabaseCommand and dropDatabaseCommand build the remote commands
// that provision/tear down a restore-test's scratch database (see
// plugin.RestoreTestSuffix).
func createDatabaseCommand(cfg Config, database string) string {
	args := append(execPrefix(cfg, false), "mariadb", "-u", cfg.Username, "-e", createDatabaseSQL(database))
	return shellcmd.Build(args)
}

func dropDatabaseCommand(cfg Config, database string) string {
	args := append(execPrefix(cfg, false), "mariadb", "-u", cfg.Username, "-e", dropDatabaseSQL(database))
	return shellcmd.Build(args)
}

func createDatabaseSQL(database string) string {
	return "CREATE DATABASE " + quoteIdentifier(database)
}

func dropDatabaseSQL(database string) string {
	return "DROP DATABASE " + quoteIdentifier(database)
}

// quoteIdentifier wraps s in backticks, doubling embedded backticks -
// MariaDB's quoted-identifier syntax. Needed for scratch names, which
// contain '-' (plugin.RestoreTestSuffix).
func quoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
