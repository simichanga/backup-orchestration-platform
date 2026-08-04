package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// remoteExecutor abstracts running a command on the target host and
// capturing its output, so command construction (dumpCommand,
// restoreCommand) is unit-testable without a real SSH server.
type remoteExecutor interface {
	Run(ctx context.Context, command string, stdin io.Reader, stdout io.Writer) error
}

// sshExecutor is the real remoteExecutor: it dials fresh per call rather
// than holding a persistent connection. A backup job runs infrequently
// (nightly, per inventory schedule), so the SSH handshake overhead is
// negligible, and dialing per call avoids adding a Close/lifecycle method
// to the BackupPlugin interface for connection teardown.
type sshExecutor struct {
	addr    string // host:port
	user    string
	keyPath string
}

func (e *sshExecutor) Run(ctx context.Context, command string, stdin io.Reader, stdout io.Writer) error {
	key, err := os.ReadFile(e.keyPath)
	if err != nil {
		return fmt.Errorf("read ssh key %s: %w", e.keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("parse ssh key %s: %w", e.keyPath, err)
	}

	config := &ssh.ClientConfig{
		User: e.user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// SECURITY (deferred, Phase 1 gap): accepts any host key, which is
		// a MITM risk. No known_hosts verification exists yet - see
		// project notes' deferred-decisions list. Must be fixed before any
		// deployment outside a fully trusted network.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", e.addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", e.addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	if stdin != nil {
		session.Stdin = stdin
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("remote command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	case <-ctx.Done():
		session.Close() // best-effort: forces the blocked Run to return
		return ctx.Err()
	}
}
