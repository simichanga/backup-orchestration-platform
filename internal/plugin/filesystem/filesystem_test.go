package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"bop/internal/core"
)

// fakeExecutor is an sshexec.Executor test double: no real SSH connection.
type fakeExecutor struct {
	lastCommand string
	lastStdin   []byte
	writeStdout string
	err         error
}

func (f *fakeExecutor) Run(_ context.Context, command string, stdin io.Reader, stdout io.Writer) error {
	f.lastCommand = command
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		f.lastStdin = b
	}
	if f.err != nil {
		return f.err
	}
	if stdout != nil && f.writeStdout != "" {
		if _, err := io.WriteString(stdout, f.writeStdout); err != nil {
			return err
		}
	}
	return nil
}

// sampleGzipArchive is a real, minimal gzip stream (empty tar content
// doesn't matter here - only the magic-byte header is exercised).
var sampleGzipArchive = []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

func TestDiscoverReturnsConfiguredPaths(t *testing.T) {
	p := &Plugin{cfg: filesystemConfig{Paths: []string{"/var/www", "/etc/myapp"}}}
	resources, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "/var/www" || resources[1].ID != "/etc/myapp" {
		t.Errorf("Discover() = %+v, want [/var/www /etc/myapp]", resources)
	}
}

func TestBackupWritesArtifact(t *testing.T) {
	exec := &fakeExecutor{writeStdout: string(sampleGzipArchive)}
	p := &Plugin{exec: exec, cfg: filesystemConfig{}, tempDir: t.TempDir()}

	artifact, err := p.Backup(context.Background(), core.Resource{ID: "/var/www"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if artifact.ResourceID != "/var/www" {
		t.Errorf("ResourceID = %q, want /var/www", artifact.ResourceID)
	}
	if artifact.Size != int64(len(sampleGzipArchive)) {
		t.Errorf("Size = %d, want %d", artifact.Size, len(sampleGzipArchive))
	}

	content, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(content) != string(sampleGzipArchive) {
		t.Errorf("artifact content mismatch")
	}

	if !strings.Contains(exec.lastCommand, "'tar'") || !strings.Contains(exec.lastCommand, "'www'") {
		t.Errorf("command = %q, expected a tar invocation for /var/www", exec.lastCommand)
	}
}

func TestBackupPropagatesExecErrorAndCleansUpFile(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("connection refused")}
	dir := t.TempDir()
	p := &Plugin{exec: exec, cfg: filesystemConfig{}, tempDir: dir}

	_, err := p.Backup(context.Background(), core.Resource{ID: "/var/www"})
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

func TestVerifyValidArchive(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/archive.tar.gz"
	if err := os.WriteFile(path, sampleGzipArchive, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifyMissingGzipMagic(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/archive.tar.gz"
	if err := os.WriteFile(path, []byte("not a gzip stream at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for a file missing the gzip magic bytes, got nil")
	}
}

func TestVerifyEmptyFile(t *testing.T) {
	p := &Plugin{}
	dir := t.TempDir()
	path := dir + "/empty.tar.gz"
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for an empty artifact, got nil")
	}
}

func TestRestoreSendsArtifactContentAsStdinToTarget(t *testing.T) {
	exec := &fakeExecutor{}
	dir := t.TempDir()
	path := dir + "/archive.tar.gz"
	if err := os.WriteFile(path, sampleGzipArchive, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec}
	if err := p.Restore(context.Background(), core.Artifact{ResourceID: "/var/www-bop-verify", Path: path}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if !bytes.Equal(exec.lastStdin, sampleGzipArchive) {
		t.Errorf("stdin sent = %v, want the artifact's content", exec.lastStdin)
	}
	if !strings.Contains(exec.lastCommand, "'/var/www-bop-verify'") {
		t.Errorf("command = %q, expected the target path %q", exec.lastCommand, "/var/www-bop-verify")
	}
	if !strings.Contains(exec.lastCommand, "'tar'") || !strings.Contains(exec.lastCommand, "'xzf'") {
		t.Errorf("command = %q, expected a tar extraction invocation", exec.lastCommand)
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
