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
	commands    []string // every command Run was called with, in order
	lastStdin   []byte
	writeStdout string
	err         error
}

func (f *fakeExecutor) Run(_ context.Context, command string, stdin io.Reader, stdout io.Writer) error {
	f.lastCommand = command
	f.commands = append(f.commands, command)
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

func TestRestoreRealTargetUsesResourceIDLiterallyWithNoCleanup(t *testing.T) {
	exec := &fakeExecutor{}
	dir := t.TempDir()
	path := dir + "/archive.tar.gz"
	if err := os.WriteFile(path, sampleGzipArchive, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Plugin{exec: exec}
	if err := p.Restore(context.Background(), core.Artifact{ResourceID: "/var/www", Path: path}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if !bytes.Equal(exec.lastStdin, sampleGzipArchive) {
		t.Errorf("stdin sent = %v, want the artifact's content", exec.lastStdin)
	}
	if len(exec.commands) != 1 {
		t.Fatalf("Run called %d times, want 1 (a real restore must never clean up its own target)", len(exec.commands))
	}
	if !strings.Contains(exec.commands[0], "'/var/www'") {
		t.Errorf("command = %q, expected the literal target path %q", exec.commands[0], "/var/www")
	}
	if !strings.Contains(exec.commands[0], "'tar'") || !strings.Contains(exec.commands[0], "'xzf'") {
		t.Errorf("command = %q, expected a tar extraction invocation", exec.commands[0])
	}
}

// TestRestoreTestRedirectsToTmpAndCleansUp locks in a fix found against a
// real SSH target: restoring as a sibling of the source (e.g.
// /var/www-bop-verify next to /var/www) requires write access in the
// source's own parent directory, which a read-only backup user commonly
// doesn't have. Redirecting under /tmp - and cleaning up afterward, so
// repeated verification runs don't accumulate copies of restored data on
// the target - avoids that.
func TestRestoreTestRedirectsToTmpAndCleansUp(t *testing.T) {
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
	if len(exec.commands) != 2 {
		t.Fatalf("Run called %d times, want 2 (extract, then cleanup)", len(exec.commands))
	}

	const wantTarget = "/tmp/bop-restore-test/var/www-bop-verify"

	extractCmd := exec.commands[0]
	if !strings.Contains(extractCmd, "'"+wantTarget+"'") {
		t.Errorf("extract command = %q, want target %q (redirected under /tmp, not a sibling of the source)", extractCmd, wantTarget)
	}
	if strings.Contains(extractCmd, "'/var/www-bop-verify'") {
		t.Errorf("extract command = %q, must not target the literal ResourceID path directly - that would require write access next to the source", extractCmd)
	}

	cleanupCmd := exec.commands[1]
	if !strings.Contains(cleanupCmd, "'rm'") || !strings.Contains(cleanupCmd, "'-rf'") {
		t.Errorf("cleanup command = %q, want an rm -rf invocation", cleanupCmd)
	}
	if !strings.Contains(cleanupCmd, "'"+wantTarget+"'") {
		t.Errorf("cleanup command = %q, want it to remove the same scratch target %q", cleanupCmd, wantTarget)
	}
}

// sequencedExecutor returns errs[call index] from each successive Run call,
// letting a test make the Nth remote command fail independently of the others.
type sequencedExecutor struct {
	errs  []error
	calls int
}

func (s *sequencedExecutor) Run(_ context.Context, _ string, stdin io.Reader, _ io.Writer) error {
	if stdin != nil {
		io.ReadAll(stdin)
	}
	err := s.errs[s.calls]
	s.calls++
	return err
}

func TestRestoreTestCleanupFailureDoesNotFailRestore(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/archive.tar.gz"
	if err := os.WriteFile(path, sampleGzipArchive, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Extract succeeds; the follow-up cleanup rm fails.
	exec := &sequencedExecutor{errs: []error{nil, errors.New("permission denied")}}
	p := &Plugin{exec: exec}

	if err := p.Restore(context.Background(), core.Artifact{ResourceID: "/var/www-bop-verify", Path: path}); err != nil {
		t.Fatalf("Restore: %v (a failed cleanup must not fail an otherwise-successful restore-test)", err)
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
