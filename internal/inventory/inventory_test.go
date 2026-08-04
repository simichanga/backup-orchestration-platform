package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

func writeInventoryFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write inventory file: %v", err)
	}
	return path
}

func TestLoadQuickstartExample(t *testing.T) {
	path := writeInventoryFile(t, `
servers:
  prod-db:
    host: 192.168.1.100
    ssh_user: bop
    ssh_key: /home/bop/.ssh/id_ed25519
    plugins:
      postgres:
        config:
          username: backup_user
          password_env: PG_BACKUP_PASSWORD
          databases:
            - myapp
    retention:
      daily: 7
      weekly: 4
      monthly: 3
    schedule: "0 3 * * *"
    verification:
      enabled: true
      target_dir: /tmp/bop-restore-test
`)

	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	srv, ok := inv.Servers["prod-db"]
	if !ok {
		t.Fatalf("Servers[prod-db] missing")
	}
	if srv.Host != "192.168.1.100" {
		t.Errorf("Host = %q, want 192.168.1.100", srv.Host)
	}
	if srv.Retention.Daily != 7 || srv.Retention.Weekly != 4 || srv.Retention.Monthly != 3 {
		t.Errorf("Retention = %+v, want daily=7 weekly=4 monthly=3", srv.Retention)
	}
	if srv.Verification == nil || !srv.Verification.Enabled || srv.Verification.TargetDir != "/tmp/bop-restore-test" {
		t.Errorf("Verification = %+v, want enabled with target_dir=/tmp/bop-restore-test", srv.Verification)
	}

	pg, ok := srv.Plugins["postgres"]
	if !ok || pg == nil {
		t.Fatalf("Plugins[postgres] missing")
	}
	if pg.Config["username"] != "backup_user" {
		t.Errorf("Plugins[postgres].Config[username] = %v, want backup_user", pg.Config["username"])
	}
}

func TestLoadPluginWithNoConfig(t *testing.T) {
	path := writeInventoryFile(t, `
servers:
  staging:
    host: 192.168.1.20
    plugins:
      docker:
    retention:
      daily: 3
`)

	inv, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	srv := inv.Servers["staging"]
	docker, ok := srv.Plugins["docker"]
	if !ok {
		t.Fatalf("Plugins[docker] missing")
	}
	if docker != nil {
		t.Errorf("Plugins[docker] = %+v, want nil (no config supplied)", docker)
	}
	if srv.Verification != nil {
		t.Errorf("Verification = %+v, want nil (no per-host override)", srv.Verification)
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "no servers",
			yaml: `servers: {}`,
		},
		{
			name: "missing host",
			yaml: `
servers:
  prod-db:
    plugins:
      postgres:
`,
		},
		{
			name: "no plugins",
			yaml: `
servers:
  prod-db:
    host: 192.168.1.100
`,
		},
		{
			name: "negative retention",
			yaml: `
servers:
  prod-db:
    host: 192.168.1.100
    plugins:
      postgres:
    retention:
      daily: -1
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeInventoryFile(t, tt.yaml)
			if _, err := Load(path); err == nil {
				t.Fatalf("Load: expected validation error, got nil")
			}
		})
	}
}
