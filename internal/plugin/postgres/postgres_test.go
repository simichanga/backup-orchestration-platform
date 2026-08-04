package postgres

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"bop/internal/core"
)

// fakeExecutor is a remoteExecutor test double: no real SSH connection.
type fakeExecutor struct {
	lastCommand string
	calls       []string
	lastStdin   []byte
	writeStdout string
	err         error
	// errFor, if set, overrides err on a per-call basis: called with each
	// command before it runs, letting a test simulate one call in a
	// sequence failing while the others succeed (e.g. restore-test's
	// create/restore/drop sequence).
	errFor func(command string) error
}

func (f *fakeExecutor) Run(_ context.Context, command string, stdin io.Reader, stdout io.Writer) error {
	f.lastCommand = command
	f.calls = append(f.calls, command)
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		f.lastStdin = b
	}
	if f.errFor != nil {
		if err := f.errFor(command); err != nil {
			return err
		}
	} else if f.err != nil {
		return f.err
	}
	if stdout != nil && f.writeStdout != "" {
		if _, err := io.WriteString(stdout, f.writeStdout); err != nil {
			return err
		}
	}
	return nil
}

const samplePgDump = "-- PostgreSQL database dump\n\nCREATE TABLE foo (id int);\n"

func TestDiscoverReturnsConfiguredDatabases(t *testing.T) {
	p := &Plugin{cfg: postgresConfig{Databases: []string{"myapp", "otherdb"}}}
	resources, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "myapp" || resources[1].ID != "otherdb" {
		t.Errorf("Discover() = %+v, want [myapp otherdb]", resources)
	}
}

func TestBackupWritesArtifact(t *testing.T) {
	exec := &fakeExecutor{writeStdout: samplePgDump}
	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "backup_user", Password: "secret"}, tempDir: t.TempDir()}

	artifact, err := p.Backup(context.Background(), core.Resource{ID: "myapp"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if artifact.ResourceID != "myapp" {
		t.Errorf("ResourceID = %q, want myapp", artifact.ResourceID)
	}
	if artifact.Size != int64(len(samplePgDump)) {
		t.Errorf("Size = %d, want %d", artifact.Size, len(samplePgDump))
	}

	content, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(content) != samplePgDump {
		t.Errorf("artifact content = %q, want %q", content, samplePgDump)
	}

	if !strings.Contains(exec.lastCommand, "pg_dump") || !strings.Contains(exec.lastCommand, "'myapp'") {
		t.Errorf("command = %q, expected a pg_dump invocation for myapp", exec.lastCommand)
	}
}

func TestBackupPropagatesExecErrorAndCleansUpFile(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("connection refused")}
	dir := t.TempDir()
	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "u", Password: "p"}, tempDir: dir}

	_, err := p.Backup(context.Background(), core.Resource{ID: "myapp"})
	if err == nil {
		t.Fatalf("Backup: expected error, got nil")
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("tempDir has %d leftover files after a failed Backup, want 0: %v", len(entries), entries)
	}
}

func TestVerifyValidDump(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte(samplePgDump), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifyMissingHeaderMarker(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte("not a real dump at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for a file missing the pg_dump header, got nil")
	}
}

func TestVerifyEmptyFile(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/empty.sql"
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for an empty artifact, got nil")
	}
}

func TestRestoreSendsArtifactContentAsStdin(t *testing.T) {
	exec := &fakeExecutor{}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte(samplePgDump), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "u", Password: "p"}}
	if err := p.Restore(context.Background(), core.Artifact{ResourceID: "myapp", Path: path}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if string(exec.lastStdin) != samplePgDump {
		t.Errorf("stdin sent = %q, want the artifact's content", exec.lastStdin)
	}
	if !strings.Contains(exec.lastCommand, "psql") {
		t.Errorf("command = %q, expected a psql invocation", exec.lastCommand)
	}
	if len(exec.calls) != 1 {
		t.Errorf("calls = %v, want exactly 1 (an ordinary restore must not provision or drop any database)", exec.calls)
	}
}

func classifyCall(command string) string {
	switch {
	case strings.Contains(command, "CREATE DATABASE"):
		return "create"
	case strings.Contains(command, "DROP DATABASE"):
		return "drop"
	case strings.Contains(command, "psql"):
		return "restore"
	default:
		return "other"
	}
}

func TestRestoreTestProvisionsAndDropsScratchDatabase(t *testing.T) {
	exec := &fakeExecutor{}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte(samplePgDump), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "u", Password: "p"}}
	if err := p.Restore(context.Background(), core.Artifact{ResourceID: "myapp-bop-verify", Path: path}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	var got []string
	for _, c := range exec.calls {
		got = append(got, classifyCall(c))
	}
	want := []string{"create", "restore", "drop"}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("calls = %v, want %v", got, want)
			break
		}
	}
	if !strings.Contains(exec.calls[0], `"myapp-bop-verify"`) {
		t.Errorf("create call = %q, want it to name myapp-bop-verify", exec.calls[0])
	}
}

func TestRestoreTestRefusesPreexistingDatabase(t *testing.T) {
	exec := &fakeExecutor{errFor: func(command string) error {
		if strings.Contains(command, "CREATE DATABASE") {
			return errors.New(`ERROR:  database "myapp-bop-verify" already exists`)
		}
		return nil
	}}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte(samplePgDump), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "u", Password: "p"}}
	err := p.Restore(context.Background(), core.Artifact{ResourceID: "myapp-bop-verify", Path: path})
	if err == nil {
		t.Fatalf("Restore: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Restore error = %q, want it to explain the database already existed", err)
	}
	if len(exec.calls) != 1 {
		t.Errorf("calls = %v, want exactly 1 (must not restore into or drop a database it didn't create)", exec.calls)
	}
}

func TestRestoreTestDropsScratchDatabaseEvenWhenRestoreFails(t *testing.T) {
	exec := &fakeExecutor{errFor: func(command string) error {
		if classifyCall(command) == "restore" {
			return errors.New("psql: connection reset")
		}
		return nil
	}}
	dir := t.TempDir()
	path := dir + "/dump.sql"
	if err := os.WriteFile(path, []byte(samplePgDump), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec, cfg: postgresConfig{Username: "u", Password: "p"}}
	err := p.Restore(context.Background(), core.Artifact{ResourceID: "myapp-bop-verify", Path: path})
	if err == nil {
		t.Fatalf("Restore: expected an error, got nil")
	}

	var got []string
	for _, c := range exec.calls {
		got = append(got, classifyCall(c))
	}
	want := []string{"create", "restore", "drop"}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v (cleanup must still run after a failed restore)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("calls = %v, want %v", got, want)
			break
		}
	}
}

func TestHealthRunsEchoOk(t *testing.T) {
	exec := &fakeExecutor{}
	p := &Plugin{exec: exec}

	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if exec.lastCommand != "echo ok" {
		t.Errorf("command = %q, want %q", exec.lastCommand, "echo ok")
	}
}

func TestHealthPropagatesError(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("no route to host")}
	p := &Plugin{exec: exec}

	if err := p.Health(context.Background()); err == nil {
		t.Errorf("Health: expected error, got nil")
	}
}
