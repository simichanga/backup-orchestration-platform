// Package sshexec runs a shell command on a remote host over SSH,
// capturing its output. Shared by every SSH-based plugin (postgres,
// filesystem, ...) that connects using inventory.yaml's ssh_user/ssh_key,
// so the connection and host-key-verification story lives in exactly one
// place rather than diverging copies.
package sshexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Executor abstracts running a command on the target host and capturing
// its output, so plugin command construction is unit-testable without a
// real SSH server.
type Executor interface {
	Run(ctx context.Context, command string, stdin io.Reader, stdout io.Writer) error
}

// SSHExecutor is the real Executor: it dials fresh per call rather than
// holding a persistent connection. A backup job runs infrequently
// (nightly, per inventory schedule), so the SSH handshake overhead is
// negligible, and dialing per call avoids adding a Close/lifecycle method
// to the BackupPlugin interface for connection teardown.
type SSHExecutor struct {
	Addr    string // host:port
	User    string
	KeyPath string
	// KnownHostsFile is an OpenSSH known_hosts-format file (config.yaml's
	// ssh.known_hosts_file) verified against every connection. There is no
	// insecure fallback: a host absent from this file, or present with a
	// different key than the one the target actually presents (a possible
	// MITM), is a connection error, not a silent accept. Operators add a
	// host's key with e.g. `ssh-keyscan -H <host> >> known_hosts` before
	// BOP can connect to it - the same trust model `ssh` itself uses.
	KnownHostsFile string
}

func (e *SSHExecutor) Run(ctx context.Context, command string, stdin io.Reader, stdout io.Writer) error {
	key, err := os.ReadFile(e.KeyPath)
	if err != nil {
		return fmt.Errorf("read ssh key %s: %w", e.KeyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("parse ssh key %s: %w", e.KeyPath, err)
	}

	hostKeyCallback, err := knownhosts.New(e.KnownHostsFile)
	if err != nil {
		return fmt.Errorf("load known_hosts file %s: %w (add this host's key first, e.g. via ssh-keyscan)", e.KnownHostsFile, err)
	}

	config := &ssh.ClientConfig{
		User:            e.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", e.Addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", e.Addr, err)
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
