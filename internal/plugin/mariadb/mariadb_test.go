package mariadb

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bop/internal/core"
)

// fakeExecutor is a sshexec.Executor test double: no real SSH connection.
type fakeExecutor struct {
	calls       []string
	lastStdin   []byte
	writeStdout string
	// errFor, if set, is called with each command before it runs, letting
	// a test fail one call in a sequence.
	errFor func(command string) error
}

func (f *fakeExecutor) Run(_ context.Context, command string, stdin io.Reader, stdout io.Writer) error {
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
	}
	if stdout != nil && f.writeStdout != "" {
		if _, err := io.WriteString(stdout, f.writeStdout); err != nil {
			return err
		}
	}
	return nil
}

// sampleDump mirrors mariadb-dump 11.x's real framing: a sandbox-mode
// line before the header comment, and the "Dump completed" trailer.
const sampleDump = "/*M!999999\\- enable the sandbox mode */ \n-- MariaDB dump 10.19-11.4.12-MariaDB, for debian-linux-gnu (x86_64)\n--\n-- Host: localhost    Database: myapp\n\nCREATE TABLE foo (id int);\n\n-- Dump completed on 2026-10-29 10:00:00\n"

func TestDiscoverReturnsConfiguredDatabases(t *testing.T) {
	p := &Plugin{cfg: Config{Databases: []string{"myapp", "otherdb"}}}
	resources, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "myapp" || resources[1].ID != "otherdb" {
		t.Errorf("Discover() = %+v, want [myapp otherdb]", resources)
	}
}

func TestBackupWritesArtifact(t *testing.T) {
	exec := &fakeExecutor{writeStdout: sampleDump}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}, tempDir: t.TempDir()}

	artifact, err := p.Backup(context.Background(), core.Resource{ID: "myapp"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if artifact.ResourceID != "myapp" || artifact.Size != int64(len(sampleDump)) {
		t.Errorf("artifact = %+v, want ResourceID myapp, Size %d", artifact, len(sampleDump))
	}
	got, err := os.ReadFile(artifact.Path)
	if err != nil || string(got) != sampleDump {
		t.Errorf("artifact contents = %q (err %v), want sample dump", got, err)
	}
	if err := p.Verify(context.Background(), artifact); err != nil {
		t.Errorf("Verify(real-shaped dump): %v", err)
	}
}

func TestBackupFailureRemovesArtifact(t *testing.T) {
	dir := t.TempDir()
	exec := &fakeExecutor{errFor: func(string) error { return errors.New("Access denied") }}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}, tempDir: dir}

	if _, err := p.Backup(context.Background(), core.Resource{ID: "myapp"}); err == nil {
		t.Fatal("Backup() = nil error, want error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("temp dir has %d leftover files, want 0", len(entries))
	}
}

func writeArtifact(t *testing.T, content string) core.Artifact {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dump.sql")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return core.Artifact{ResourceID: "myapp", Path: path, Size: int64(len(content))}
}

func TestVerify(t *testing.T) {
	long := strings.Repeat("INSERT INTO foo VALUES (1);\n", 1000)
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"valid", sampleDump, ""},
		{"valid larger than window", "-- MariaDB dump 10.19\n" + long + "-- Dump completed on x\n", ""},
		{"empty", "", "empty"},
		{"not a dump", "hello world\n-- Dump completed\n", "missing \"MariaDB dump\""},
		{"truncated", "-- MariaDB dump 10.19\n" + long, "truncated"},
	}
	p := &Plugin{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Verify(context.Background(), writeArtifact(t, tt.content))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Verify() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Verify() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRestoreLiveDoesNotCreateOrDrop(t *testing.T) {
	exec := &fakeExecutor{}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}}

	if err := p.Restore(context.Background(), writeArtifact(t, sampleDump)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(exec.calls) != 1 || !strings.Contains(exec.calls[0], "'mariadb' '-u' 'root' 'myapp'") {
		t.Errorf("calls = %q, want a single restore into myapp", exec.calls)
	}
	if string(exec.lastStdin) != sampleDump {
		t.Errorf("stdin = %q, want the dump", exec.lastStdin)
	}
}

func TestRestoreTestCreatesAndDropsScratch(t *testing.T) {
	exec := &fakeExecutor{}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}}
	a := writeArtifact(t, sampleDump)
	a.ResourceID = "myapp-bop-verify"

	if err := p.Restore(context.Background(), a); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(exec.calls) != 3 ||
		!strings.Contains(exec.calls[0], "CREATE DATABASE `myapp-bop-verify`") ||
		!strings.Contains(exec.calls[1], "'myapp-bop-verify'") ||
		!strings.Contains(exec.calls[2], "DROP DATABASE `myapp-bop-verify`") {
		t.Errorf("calls = %q, want create/restore/drop of the scratch database", exec.calls)
	}
}

func TestRestoreTestRefusesPreexistingScratch(t *testing.T) {
	exec := &fakeExecutor{errFor: func(cmd string) error {
		if strings.Contains(cmd, "CREATE DATABASE") {
			return errors.New("ERROR 1007 (HY000): Can't create database 'myapp-bop-verify'; database exists")
		}
		return nil
	}}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}}
	a := writeArtifact(t, sampleDump)
	a.ResourceID = "myapp-bop-verify"

	err := p.Restore(context.Background(), a)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("Restore() = %v, want refusal", err)
	}
	if len(exec.calls) != 1 {
		t.Errorf("calls = %q, want only the failed CREATE (no restore, no drop)", exec.calls)
	}
}

func TestRestoreTestDropsScratchOnRestoreFailure(t *testing.T) {
	exec := &fakeExecutor{errFor: func(cmd string) error {
		if strings.HasSuffix(cmd, "'myapp-bop-verify'") {
			return errors.New("syntax error")
		}
		return nil
	}}
	p := &Plugin{exec: exec, cfg: Config{Username: "root", Password: "secret"}}
	a := writeArtifact(t, sampleDump)
	a.ResourceID = "myapp-bop-verify"

	if err := p.Restore(context.Background(), a); err == nil {
		t.Fatal("Restore() = nil, want error")
	}
	if len(exec.calls) != 3 || !strings.Contains(exec.calls[2], "DROP DATABASE") {
		t.Errorf("calls = %q, want scratch dropped after failed restore", exec.calls)
	}
}

func TestMetadata(t *testing.T) {
	if got := (&Plugin{}).Metadata(); got.Name != "mariadb" {
		t.Errorf("Metadata().Name = %q, want mariadb", got.Name)
	}
}
