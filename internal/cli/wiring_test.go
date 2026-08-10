package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bop/internal/events"
)

func writeTestFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestBuildAppPostgresConnectionFailureIsCleanAndRedacted covers what used
// to be TestBuildAppRejectsUnsupportedMetadataDriver, retired now that
// metadata.driver: postgres is real (internal/config's own
// TestLoadValidationErrors covers a genuinely unsupported driver name,
// which config.Load rejects before buildApp is ever reached). What's worth
// testing here instead: a real Postgres connection failure (host
// unreachable, not a config typo) must surface as a clean error through
// buildApp, and must never leak the DSN's password into that error.
func TestBuildAppPostgresConnectionFailureIsCleanAndRedacted(t *testing.T) {
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
  driver: postgres
  dsn: "postgres://bopuser:s3cret-password@127.0.0.1:1/bop?connect_timeout=2"
`)

	_, err := buildApp(cfgPath)
	if err == nil {
		t.Fatalf("buildApp against an unreachable Postgres: expected an error, got nil")
	}
	if strings.Contains(err.Error(), "s3cret-password") {
		t.Errorf("buildApp error leaked the DSN password: %v", err)
	}
	if !strings.Contains(err.Error(), "open metadata store") {
		t.Errorf("buildApp error = %v, want it to mention opening the metadata store", err)
	}
}

func TestBuildAppRejectsUnsupportedStorageProvider(t *testing.T) {
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
  provider: borg
metadata:
  driver: sqlite
  dsn: ":memory:"
`)

	_, err := buildApp(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "storage.provider") {
		t.Errorf("buildApp with storage.provider=borg: err = %v, want a storage.provider error", err)
	}
}

func TestBuildAppSucceedsWithSupportedDrivers(t *testing.T) {
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
`)

	a, err := buildApp(cfgPath)
	if err != nil {
		t.Fatalf("buildApp: %v", err)
	}
	defer a.Close()

	if len(a.Inventory.Servers) != 1 {
		t.Errorf("Inventory.Servers = %d, want 1", len(a.Inventory.Servers))
	}
}

// TestBuildAppPersistsEventsThroughController proves metadata.EventPublisher
// is actually reachable through buildApp's events.Multi fan-out - not just
// that EventPublisher.Publish works in isolation (see
// internal/metadata/events_test.go), but that wiring.go's Subscribers list
// really includes it. A typo or dropped entry there wouldn't be caught by
// the metadata package's own tests.
func TestBuildAppPersistsEventsThroughController(t *testing.T) {
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
`)

	a, err := buildApp(cfgPath)
	if err != nil {
		t.Fatalf("buildApp: %v", err)
	}
	defer a.Close()

	a.Controller.Events.Publish(context.Background(), events.Event{
		Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres",
		Timestamp: time.Now().UTC(),
	})

	got, err := a.Metadata.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "job-1" {
		t.Fatalf("ListEvents = %+v, want the published event persisted via the real controller wiring", got)
	}
}
