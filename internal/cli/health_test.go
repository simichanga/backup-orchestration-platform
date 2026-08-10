package cli

import (
	"strings"
	"testing"
)

func TestHealthCmdRequiresHostAndPlugin(t *testing.T) {
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
	root.SetArgs([]string{"--config", cfgPath, "health"})
	if err := root.Execute(); err == nil {
		t.Fatalf("health with no --host/--plugin: expected an error, got nil")
	}
}

func TestHealthCmdUnknownHost(t *testing.T) {
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
	root.SetArgs([]string{"--config", cfgPath, "health", "--host", "no-such-host", "--plugin", "postgres"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no-such-host") {
		t.Fatalf("health --host no-such-host: err = %v, want a host-not-found error", err)
	}
}

func TestHealthCmdUnknownPlugin(t *testing.T) {
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
	root.SetArgs([]string{"--config", cfgPath, "health", "--host", "prod-db", "--plugin", "no-such-plugin"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no-such-plugin") {
		t.Fatalf("health --plugin no-such-plugin: err = %v, want a plugin-not-registered error", err)
	}
}

// TestHealthCmdPluginNotConfiguredForHost is the case docs/08-roadmap.md
// used to flag as a known nit: postgres is registered as a plugin type and
// works fine on other hosts, but this specific host's inventory entry never
// lists it under `plugins:`. Before the fix, srv.Plugins["postgres"]'s
// missing-key zero value (nil) got passed straight to the postgres
// factory, which rejected it exactly like malformed config would
// ("postgres: no config provided", doubly wrapped) - indistinguishable
// from a real config error. Controller.BuildPlugin now checks the key's
// presence before ever reaching the factory.
func TestHealthCmdPluginNotConfiguredForHost(t *testing.T) {
	dir := t.TempDir()
	invPath := writeTestFile(t, dir, "inventory.yaml", `
servers:
  prod-db:
    host: 192.168.1.100
    plugins:
      filesystem:
        config:
          paths: ["/data"]
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
	root.SetArgs([]string{"--config", cfgPath, "health", "--host", "prod-db", "--plugin", "postgres"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("health --plugin postgres on a host that never lists it: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "not configured for host") {
		t.Errorf("health error = %v, want a clear \"not configured for host\" message, not a raw config-parse error", err)
	}
}

// TestHealthCmdPropagatesPluginHealthError uses a real postgres config (so
// BuildPlugin succeeds) pointed at 192.168.1.100 - the same
// assumed-unreachable-in-this-sandbox address controller_test.go's fixtures
// already rely on - so Health's real SSH dial fails. Proves the command
// actually calls Health() and surfaces its error with host/plugin context,
// not just that plugin construction succeeds.
func TestHealthCmdPropagatesPluginHealthError(t *testing.T) {
	dir := t.TempDir()
	invPath := writeTestFile(t, dir, "inventory.yaml", `
servers:
  prod-db:
    host: 192.168.1.100
    ssh_user: nobody
    ssh_key: `+dir+`/id_ed25519
    plugins:
      postgres:
        config:
          username: bopuser
          password_env: BOP_TEST_HEALTH_PASSWORD
          databases: [testdb]
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
	t.Setenv("BOP_TEST_HEALTH_PASSWORD", "unused")
	writeTestFile(t, dir, "id_ed25519", "not a real key, only the path needs to exist for plugin construction")

	root := NewRootCmd()
	root.SetArgs([]string{"--config", cfgPath, "health", "--host", "prod-db", "--plugin", "postgres"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("health against an unreachable host: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "prod-db") || !strings.Contains(err.Error(), "postgres") {
		t.Errorf("health error = %v, want host/plugin context (prod-db/postgres)", err)
	}
}
