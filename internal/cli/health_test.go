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
