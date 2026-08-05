package cli

import (
	"context"
	"testing"
	"time"

	"bop/internal/events"
)

// TestRunEventPrunerPrunesImmediatelyOnStart passes an already-cancelled
// context: runEventPruner runs its prune() once before checking ctx (see
// wiring.go), so calling it synchronously this way is deterministic - no
// sleep/goroutine-scheduling race needed to observe the immediate prune.
func TestRunEventPrunerPrunesImmediatelyOnStart(t *testing.T) {
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
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
metadata:
  driver: sqlite
  dsn: ":memory:"
  event_retention: 1h
`)

	a, err := buildApp(cfgPath)
	if err != nil {
		t.Fatalf("buildApp: %v", err)
	}
	defer a.Close()

	old := events.Event{Type: events.TypeBackupCompleted, Host: "prod-db", Timestamp: time.Now().UTC().Add(-100 * time.Hour)}
	recent := events.Event{Type: events.TypeBackupCompleted, Host: "prod-db", Timestamp: time.Now().UTC()}
	for _, e := range []events.Event{old, recent} {
		if err := a.Metadata.RecordEvent(context.Background(), e); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.runEventPruner(ctx)

	got, err := a.Metadata.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || !got[0].Timestamp.Equal(recent.Timestamp) {
		t.Fatalf("ListEvents after pruning = %+v, want just the recent event", got)
	}
}
