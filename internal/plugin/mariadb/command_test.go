package mariadb

import (
	"strings"
	"testing"
)

func TestDumpCommandDirect(t *testing.T) {
	cfg := Config{Username: "root", Password: "secret"}
	got := dumpCommand(cfg, "myapp")

	for _, want := range []string{"'mariadb-dump'", "'-u'", "'root'", "'--single-transaction'", "'--routines'", "'myapp'", "MYSQL_PWD=secret"} {
		if !strings.Contains(got, want) {
			t.Errorf("dumpCommand() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "docker") {
		t.Errorf("dumpCommand() = %q, should not invoke docker without a container configured", got)
	}
	if strings.Contains(got, "--databases") {
		t.Errorf("dumpCommand() = %q, must not use --databases (dump would hardcode USE <db>, breaking restore into a scratch db)", got)
	}
}

func TestDumpCommandContainerWithPassword(t *testing.T) {
	cfg := Config{Username: "root", Password: "secret", Container: "my-mariadb"}
	got := dumpCommand(cfg, "myapp")

	if !strings.HasPrefix(got, "'docker' 'exec' '-e' 'MYSQL_PWD=secret' 'my-mariadb' 'mariadb-dump'") {
		t.Errorf("dumpCommand() = %q", got)
	}
}

func TestDumpCommandContainerEnvKeepsSecretOffCommandLine(t *testing.T) {
	cfg := Config{Username: "root", Container: "my-mariadb", PasswordContainerEnv: "MARIADB_ROOT_PASSWORD"}
	got := dumpCommand(cfg, "myapp")

	want := `'docker' 'exec' 'my-mariadb' 'sh' '-c' 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec "$@"' 'sh' 'mariadb-dump'`
	if !strings.HasPrefix(got, want) {
		t.Errorf("dumpCommand() = %q, want prefix %q", got, want)
	}
	if strings.Contains(got, "'-e'") {
		t.Errorf("dumpCommand() = %q, should not pass the password via docker exec -e", got)
	}
}

func TestRestoreCommandContainerUsesInteractiveFlag(t *testing.T) {
	cfg := Config{Username: "root", Container: "my-mariadb", PasswordContainerEnv: "MARIADB_ROOT_PASSWORD"}
	got := restoreCommand(cfg, "myapp")

	if !strings.HasPrefix(got, "'docker' 'exec' '-i' 'my-mariadb'") {
		t.Errorf("restoreCommand() = %q, want docker exec -i (stdin must stay open for mariadb)", got)
	}
	if !strings.HasSuffix(got, "'mariadb' '-u' 'root' 'myapp'") {
		t.Errorf("restoreCommand() = %q, want mariadb client into myapp", got)
	}
}

func TestCreateAndDropDatabaseCommands(t *testing.T) {
	cfg := Config{Username: "root", Password: "secret"}

	if got := createDatabaseCommand(cfg, "myapp-bop-verify"); !strings.Contains(got, "'CREATE DATABASE `myapp-bop-verify`'") {
		t.Errorf("createDatabaseCommand() = %q", got)
	}
	if got := dropDatabaseCommand(cfg, "myapp-bop-verify"); !strings.Contains(got, "'DROP DATABASE `myapp-bop-verify`'") {
		t.Errorf("dropDatabaseCommand() = %q", got)
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	if got := quoteIdentifier("a`b"); got != "`a``b`" {
		t.Errorf("quoteIdentifier() = %q, want %q", got, "`a``b`")
	}
}
