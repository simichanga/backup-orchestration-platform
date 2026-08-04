package cli

import (
	"context"
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
