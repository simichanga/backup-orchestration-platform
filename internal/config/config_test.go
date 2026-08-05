package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
`)

	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Controller.Concurrency != 4 {
		t.Errorf("Controller.Concurrency = %d, want 4", cfg.Controller.Concurrency)
	}
	if cfg.Controller.JobTimeout != 2*time.Hour {
		t.Errorf("Controller.JobTimeout = %v, want 2h", cfg.Controller.JobTimeout)
	}
	if cfg.Metadata.Driver != "sqlite" {
		t.Errorf("Metadata.Driver = %q, want sqlite", cfg.Metadata.Driver)
	}
	if cfg.Verification.Enabled {
		t.Errorf("Verification.Enabled = true, want false")
	}
	if cfg.SSH.KnownHostsFile != "/etc/bop/known_hosts" {
		t.Errorf("SSH.KnownHostsFile = %q, want /etc/bop/known_hosts", cfg.SSH.KnownHostsFile)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
controller:
  concurrency: 4
`)

	t.Setenv("BOP_CONTROLLER_CONCURRENCY", "9")

	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Controller.Concurrency != 9 {
		t.Errorf("Controller.Concurrency = %d, want 9 (env override)", cfg.Controller.Concurrency)
	}
}

func TestLoadResticPasswordEnv(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_env: RESTIC_REPO_PASSWORD
`)

	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Restic.PasswordEnv != "RESTIC_REPO_PASSWORD" {
		t.Errorf("Storage.Restic.PasswordEnv = %q, want RESTIC_REPO_PASSWORD", cfg.Storage.Restic.PasswordEnv)
	}
	if cfg.Storage.Restic.PasswordFile != "" {
		t.Errorf("Storage.Restic.PasswordFile = %q, want empty", cfg.Storage.Restic.PasswordFile)
	}
}

func TestLoadAPIDisabledByDefaultNeedsNoTokenSource(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.API.Enabled {
		t.Error("API.Enabled = true, want false (default)")
	}
}

func TestLoadAPIEnabledWithTokenEnv(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
api:
  enabled: true
  token_env: BOP_API_TOKEN
`)
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.API.Enabled {
		t.Error("API.Enabled = false, want true")
	}
	if cfg.API.TokenEnv != "BOP_API_TOKEN" {
		t.Errorf("API.TokenEnv = %q, want BOP_API_TOKEN", cfg.API.TokenEnv)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "unsupported storage provider",
			yaml: `
storage:
  provider: borg
`,
		},
		{
			name: "missing restic repository",
			yaml: `
storage:
  provider: restic
`,
		},
		{
			name: "missing restic password",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
`,
		},
		{
			name: "restic password_file and password_env both set",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
    password_env: RESTIC_REPO_PASSWORD
`,
		},
		{
			name: "zero controller concurrency",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
controller:
  concurrency: 0
`,
		},
		{
			name: "invalid log level",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
logging:
  level: verbose
`,
		},
		{
			name: "api enabled with no token source",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
api:
  enabled: true
`,
		},
		{
			name: "api tokens_file and token_env both set",
			yaml: `
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod
    password_file: /etc/bop/restic-password.txt
api:
  enabled: true
  tokens_file: /etc/bop/api-tokens.txt
  token_env: BOP_API_TOKEN
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.yaml)
			if _, err := Load(path, nil); err == nil {
				t.Fatalf("Load: expected validation error, got nil")
			}
		})
	}
}
