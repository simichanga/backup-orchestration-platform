package postgres

import "strings"

// shellQuote wraps s in single quotes for a POSIX shell, escaping any
// embedded single quotes. The remote SSH server runs the command line
// through the user's shell, so every token that isn't a fixed keyword
// must be quoted - database names, the password, and the container name
// are all operator-controlled inventory data, not free-form user input,
// but quoting is cheap and correct regardless.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildCommand quotes each argv-style token and joins them into a single
// shell command line. Building as an argv slice first, rather than
// interpolating into a format string, avoids the nested-quoting bugs that
// come from mixing docker exec's own arguments with the command it runs.
func buildCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// dumpCommand builds the remote command to dump database via pg_dump,
// either directly on the host or, when cfg.Container is set, inside that
// Docker container via `docker exec` - most Postgres instances in this
// deployment are containerized. pg_dump's default plain-text SQL output
// format is used deliberately, so restore only ever needs psql, not
// pg_restore.
func dumpCommand(cfg postgresConfig, database string) string {
	var args []string
	if cfg.Container != "" {
		args = append(args, "docker", "exec", "-e", "PGPASSWORD="+cfg.Password, cfg.Container)
	} else {
		args = append(args, "env", "PGPASSWORD="+cfg.Password)
	}
	args = append(args, "pg_dump", "-U", cfg.Username, database)
	return buildCommand(args)
}

// restoreCommand builds the remote command to restore database from a
// plain-text SQL dump piped in on stdin. docker exec needs -i to keep
// stdin open when a container is involved.
func restoreCommand(cfg postgresConfig, database string) string {
	var args []string
	if cfg.Container != "" {
		args = append(args, "docker", "exec", "-i", "-e", "PGPASSWORD="+cfg.Password, cfg.Container)
	} else {
		args = append(args, "env", "PGPASSWORD="+cfg.Password)
	}
	args = append(args, "psql", "-U", cfg.Username, database)
	return buildCommand(args)
}
