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

func TestShellQuoteEscapesEmbeddedQuotes(t *testing.T) {
	got := shellQuote(`o'brien`)
	want := `'o'\''brien'`
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", `o'brien`, got, want)
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
