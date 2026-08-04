package sshexec

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// These tests run a real SSH server in-process (not a mock of the ssh
// package) so host-key verification is exercised against the actual SSH
// protocol handshake, not just trusted from reading knownhosts' docs.

func writeClientKey(t *testing.T, dir string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(dir, "client_key")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write client key: %v", err)
	}
	return path
}

func generateHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer from host key: %v", err)
	}
	return signer
}

// startTestSSHServer starts a minimal SSH server on 127.0.0.1 that accepts
// any client (NoClientAuth) - these tests exercise the client's host-key
// verification, not authentication - and for an "exec" request writes a
// fixed line to the channel before reporting success. Returns the
// listener's address and the server's real host key.
func startTestSSHServer(t *testing.T) (addr string, hostSigner ssh.Signer) {
	t.Helper()
	hostSigner = generateHostKey(t)

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConn(conn, config)
		}
	}()

	return ln.Addr().String(), hostSigner
}

func serveTestSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			// Handle exactly one exec request, then close the channel:
			// the client's Session.Wait() blocks until this side closes
			// the channel, so looping over "requests" indefinitely (only
			// exiting once the client closes its end) deadlocks - each
			// side would be waiting on the other.
			for req := range requests {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				io.WriteString(ch, "ok\n")
				ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				req.Reply(true, nil)
				return
			}
		}()
	}
}

func writeKnownHostsFile(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "known_hosts")
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

func TestRunSucceedsWhenHostKeyMatchesKnownHosts(t *testing.T) {
	dir := t.TempDir()
	addr, hostSigner := startTestSSHServer(t)
	knownHosts := writeKnownHostsFile(t, dir, knownhosts.Line([]string{addr}, hostSigner.PublicKey()))
	clientKey := writeClientKey(t, dir)

	e := &SSHExecutor{Addr: addr, User: "test", KeyPath: clientKey, KnownHostsFile: knownHosts}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out strings.Builder
	if err := e.Run(ctx, "echo ok", nil, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "ok\n" {
		t.Errorf("output = %q, want %q", out.String(), "ok\n")
	}
}

func TestRunFailsWhenHostIsNotInKnownHosts(t *testing.T) {
	dir := t.TempDir()
	addr, _ := startTestSSHServer(t)
	knownHosts := writeKnownHostsFile(t, dir) // empty
	clientKey := writeClientKey(t, dir)

	e := &SSHExecutor{Addr: addr, User: "test", KeyPath: clientKey, KnownHostsFile: knownHosts}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := e.Run(ctx, "echo ok", nil, io.Discard)
	if err == nil {
		t.Fatal("Run: expected an error for a host absent from known_hosts, got nil")
	}
}

// TestRunFailsWhenHostKeyMismatchesKnownHosts is the actual MITM scenario
// InsecureIgnoreHostKey used to silently accept: known_hosts has an entry
// for this address, but it names a different key than the one the server
// actually presents.
func TestRunFailsWhenHostKeyMismatchesKnownHosts(t *testing.T) {
	dir := t.TempDir()
	addr, _ := startTestSSHServer(t) // server's real key is discarded here
	wrongSigner := generateHostKey(t)
	knownHosts := writeKnownHostsFile(t, dir, knownhosts.Line([]string{addr}, wrongSigner.PublicKey()))
	clientKey := writeClientKey(t, dir)

	e := &SSHExecutor{Addr: addr, User: "test", KeyPath: clientKey, KnownHostsFile: knownHosts}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := e.Run(ctx, "echo ok", nil, io.Discard)
	if err == nil {
		t.Fatal("Run: expected an error for a mismatched host key (possible MITM), got nil")
	}
}

func TestRunFailsFastWhenKnownHostsFileMissing(t *testing.T) {
	dir := t.TempDir()
	addr, _ := startTestSSHServer(t)
	clientKey := writeClientKey(t, dir)

	e := &SSHExecutor{Addr: addr, User: "test", KeyPath: clientKey, KnownHostsFile: filepath.Join(dir, "does-not-exist")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := e.Run(ctx, "echo ok", nil, io.Discard); err == nil {
		t.Fatal("Run: expected an error for a missing known_hosts file, got nil")
	}
}
