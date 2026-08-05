package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestControllerCmdStartsAndShutsDownOnContextCancellation runs the real
// controller command (buildApp, FailOrphanedJobs, reconciliation,
// scheduler, consumer) against a scratch config/inventory, then cancels
// its context the way signal.NotifyContext would on SIGINT/SIGTERM. It
// must return promptly with no error, proving startup wiring succeeds and
// shutdown is clean rather than hanging.
func TestControllerCmdStartsAndShutsDownOnContextCancellation(t *testing.T) {
	dir := t.TempDir()
	invPath := writeTestFile(t, dir, "inventory.yaml", `
servers:
  prod-db:
    host: 192.168.1.100
    plugins:
      postgres:
    retention:
      daily: 7
`)
	cfgPath := writeTestFile(t, dir, "config.yaml", `
inventory: `+invPath+`
storage:
  provider: restic
  restic:
    repository: `+dir+`/repo
    password_file: `+dir+`/restic-password.txt
metadata:
  driver: sqlite
  dsn: ":memory:"
`)

	root := NewRootCmd()
	root.SetArgs([]string{"--config", cfgPath, "controller"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("controller command returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("controller command did not shut down after its context was cancelled")
	}
}

// TestControllerCmdServesMetricsEndpoint proves the Prometheus /metrics
// endpoint is actually reachable over a real HTTP connection during a live
// "bop controller" run, not just wired in code that never gets exercised.
func TestControllerCmdServesMetricsEndpoint(t *testing.T) {
	dir := t.TempDir()
	invPath := writeTestFile(t, dir, "inventory.yaml", `
servers:
  prod-db:
    host: 192.168.1.100
    plugins:
      postgres:
    retention:
      daily: 7
`)
	cfgPath := writeTestFile(t, dir, "config.yaml", `
inventory: `+invPath+`
storage:
  provider: restic
  restic:
    repository: `+dir+`/repo
    password_file: `+dir+`/restic-password.txt
metadata:
  driver: sqlite
  dsn: ":memory:"
metrics:
  port: 19091
  path: /metrics
`)

	root := NewRootCmd()
	root.SetArgs([]string{"--config", cfgPath, "controller"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	var resp *http.Response
	var lastErr error
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		resp, lastErr = http.Get("http://127.0.0.1:19091/metrics")
		if lastErr == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("GET /metrics never succeeded: %v", lastErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("controller command returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("controller command did not shut down")
	}
}

// TestControllerCmdAPITriggersRealBackupJob proves the API server's wiring
// in newControllerCmd (a.Controller, a.Queue passed to api.NewServer) is
// actually correct, not just compiled - a mistake there wouldn't be caught
// by internal/api's own tests, which construct a Controller/Queue directly
// rather than going through buildApp. Also exercises the read/write token
// split end-to-end through the real CLI command.
func TestControllerCmdAPITriggersRealBackupJob(t *testing.T) {
	t.Setenv("BOP_TEST_PG_PASSWORD", "supersecret")

	dir := t.TempDir()
	invPath := writeTestFile(t, dir, "inventory.yaml", `
servers:
  prod-db:
    host: 192.168.1.100
    ssh_user: bop
    ssh_key: `+dir+`/id_ed25519
    plugins:
      postgres:
        config:
          username: backup_user
          password_env: BOP_TEST_PG_PASSWORD
          databases:
            - myapp
    retention:
      daily: 7
`)
	readTokensPath := writeTestFile(t, dir, "tokens.txt", "read-token\n")
	writeTokensPath := writeTestFile(t, dir, "write-tokens.txt", "write-token\n")
	cfgPath := writeTestFile(t, dir, "config.yaml", `
inventory: `+invPath+`
storage:
  provider: restic
  restic:
    repository: `+dir+`/repo
    password_file: `+dir+`/restic-password.txt
metadata:
  driver: sqlite
  dsn: ":memory:"
api:
  enabled: true
  addr: "127.0.0.1:19092"
  tokens_file: `+readTokensPath+`
  write_tokens_file: `+writeTokensPath+`
`)

	root := NewRootCmd()
	root.SetArgs([]string{"--config", cfgPath, "controller"})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	deadline := time.Now().Add(1 * time.Second)
	var apiUp bool
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://127.0.0.1:19092/v1/hosts"); err == nil {
			resp.Body.Close()
			apiUp = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !apiUp {
		t.Fatal("API server never came up")
	}

	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:19092/v1/backups", strings.NewReader(`{"host":"prod-db","plugin":"postgres"}`))
	req.Header.Set("Authorization", "Bearer read-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/backups (read token): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("read token: status = %d, want 401", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, "http://127.0.0.1:19092/v1/backups", strings.NewReader(`{"host":"prod-db","plugin":"postgres"}`))
	req.Header.Set("Authorization", "Bearer write-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/backups (write token): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("write token: status = %d, want 202 (body: %s)", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("controller command returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("controller command did not shut down")
	}
}
