package cli

import (
	"context"
	"net/http"
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
