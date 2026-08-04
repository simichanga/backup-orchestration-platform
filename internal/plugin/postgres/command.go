package postgres

import "bop/internal/plugin/shellcmd"

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
	return shellcmd.Build(args)
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
	return shellcmd.Build(args)
}
