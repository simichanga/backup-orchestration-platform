package postgres

import (
	"strings"
	"testing"
)

func TestDumpCommandDirect(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret"}
	got := dumpCommand(cfg, "myapp")

	for _, want := range []string{"'pg_dump'", "'-U'", "'backup_user'", "'myapp'", "PGPASSWORD=secret"} {
		if !strings.Contains(got, want) {
			t.Errorf("dumpCommand() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "docker") {
		t.Errorf("dumpCommand() = %q, should not invoke docker without a container configured", got)
	}
}

func TestDumpCommandContainer(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret", Container: "my-postgres"}
	got := dumpCommand(cfg, "myapp")

	for _, want := range []string{"'docker'", "'exec'", "'my-postgres'", "'pg_dump'", "'-U'", "'backup_user'", "'myapp'"} {
		if !strings.Contains(got, want) {
			t.Errorf("dumpCommand() = %q, missing %q", got, want)
		}
	}
}

func TestRestoreCommandContainerUsesInteractiveFlag(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret", Container: "my-postgres"}
	got := restoreCommand(cfg, "myapp")

	if !strings.Contains(got, "'docker' 'exec' '-i'") {
		t.Errorf("restoreCommand() = %q, want docker exec -i (stdin must stay open for psql)", got)
	}
	if !strings.Contains(got, "psql") {
		t.Errorf("restoreCommand() = %q, want psql not pg_restore (plain-format dumps)", got)
	}
}

func TestCreateDatabaseCommandDirect(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret"}
	got := createDatabaseCommand(cfg, "myapp-bop-verify")

	for _, want := range []string{"'psql'", "'-U'", "'backup_user'", "'-d'", "'postgres'", `CREATE DATABASE "myapp-bop-verify"`, "ON_ERROR_STOP=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("createDatabaseCommand() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "docker") {
		t.Errorf("createDatabaseCommand() = %q, should not invoke docker without a container configured", got)
	}
}

func TestCreateDatabaseCommandContainer(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret", Container: "my-postgres"}
	got := createDatabaseCommand(cfg, "myapp-bop-verify")

	for _, want := range []string{"'docker'", "'exec'", "'my-postgres'", `CREATE DATABASE "myapp-bop-verify"`} {
		if !strings.Contains(got, want) {
			t.Errorf("createDatabaseCommand() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "'-i'") {
		t.Errorf("createDatabaseCommand() = %q, should not use docker exec -i (no stdin needed)", got)
	}
}

func TestDropDatabaseCommand(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: "secret"}
	got := dropDatabaseCommand(cfg, "myapp-bop-verify")

	if !strings.Contains(got, `DROP DATABASE "myapp-bop-verify"`) {
		t.Errorf("dropDatabaseCommand() = %q, want a DROP DATABASE for myapp-bop-verify", got)
	}
	if !strings.Contains(got, "'-d'") || !strings.Contains(got, "'postgres'") {
		t.Errorf("dropDatabaseCommand() = %q, want it to connect to the postgres maintenance database", got)
	}
}

func TestCreateDatabaseSQLEscapesEmbeddedQuotes(t *testing.T) {
	got := createDatabaseSQL(`weird"name`)
	want := `CREATE DATABASE "weird""name"`
	if got != want {
		t.Errorf("createDatabaseSQL() = %q, want %q", got, want)
	}
}

func TestDumpCommandQuotesPasswordWithSpecialCharacters(t *testing.T) {
	cfg := postgresConfig{Username: "backup_user", Password: `p'ss word`}
	got := dumpCommand(cfg, "myapp")

	// The password must appear shell-escaped, not verbatim, or a password
	// containing a single quote would break out of its argument and be
	// interpreted as shell syntax.
	if strings.Contains(got, `PGPASSWORD=p'ss word`) {
		t.Errorf("dumpCommand() = %q, password with a quote was not shell-escaped", got)
	}
}
