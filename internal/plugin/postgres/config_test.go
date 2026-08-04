package postgres

import (
	"testing"

	"bop/internal/inventory"
)

func validRawConfig() map[string]interface{} {
	return map[string]interface{}{
		"username":     "backup_user",
		"password_env": "PG_TEST_PASSWORD",
		"databases":    []interface{}{"myapp", "otherdb"},
	}
}

func TestParseConfigValid(t *testing.T) {
	t.Setenv("PG_TEST_PASSWORD", "supersecret")

	cfg, err := parseConfig(&inventory.PluginConfig{Config: validRawConfig()})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Username != "backup_user" {
		t.Errorf("Username = %q, want backup_user", cfg.Username)
	}
	if cfg.Password != "supersecret" {
		t.Errorf("Password = %q, want supersecret", cfg.Password)
	}
	if len(cfg.Databases) != 2 || cfg.Databases[0] != "myapp" || cfg.Databases[1] != "otherdb" {
		t.Errorf("Databases = %v, want [myapp otherdb]", cfg.Databases)
	}
	if cfg.Container != "" {
		t.Errorf("Container = %q, want empty (not configured)", cfg.Container)
	}
}

func TestParseConfigWithContainer(t *testing.T) {
	t.Setenv("PG_TEST_PASSWORD", "supersecret")
	raw := validRawConfig()
	raw["container"] = "my-postgres"

	cfg, err := parseConfig(&inventory.PluginConfig{Config: raw})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Container != "my-postgres" {
		t.Errorf("Container = %q, want my-postgres", cfg.Container)
	}
}

func TestParseConfigErrors(t *testing.T) {
	t.Setenv("PG_TEST_PASSWORD", "supersecret")

	tests := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{"missing username", func(m map[string]interface{}) { delete(m, "username") }},
		{"missing password_env", func(m map[string]interface{}) { delete(m, "password_env") }},
		{"password_env not set", func(m map[string]interface{}) { m["password_env"] = "PG_TEST_UNSET_VAR" }},
		{"missing databases", func(m map[string]interface{}) { delete(m, "databases") }},
		{"databases wrong type", func(m map[string]interface{}) { m["databases"] = "myapp" }},
		{"databases element wrong type", func(m map[string]interface{}) { m["databases"] = []interface{}{1} }},
		{"empty databases list", func(m map[string]interface{}) { m["databases"] = []interface{}{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := validRawConfig()
			tt.mutate(raw)
			if _, err := parseConfig(&inventory.PluginConfig{Config: raw}); err == nil {
				t.Fatalf("parseConfig: expected error, got nil")
			}
		})
	}
}

func TestParseConfigNilConfig(t *testing.T) {
	if _, err := parseConfig(nil); err == nil {
		t.Fatalf("parseConfig(nil): expected error, got nil")
	}
}
