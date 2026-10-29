package mariadb

import (
	"strings"
	"testing"

	"bop/internal/inventory"
)

func validRawConfig() map[string]interface{} {
	return map[string]interface{}{
		"username":     "root",
		"password_env": "MARIADB_TEST_PASSWORD",
		"databases":    []interface{}{"myapp", "otherdb"},
	}
}

func TestParseConfigValid(t *testing.T) {
	t.Setenv("MARIADB_TEST_PASSWORD", "supersecret")

	cfg, err := ParseConfig(&inventory.PluginConfig{Config: validRawConfig()})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Username != "root" {
		t.Errorf("Username = %q, want root", cfg.Username)
	}
	if cfg.Password != "supersecret" {
		t.Errorf("Password = %q, want supersecret", cfg.Password)
	}
	if len(cfg.Databases) != 2 || cfg.Databases[0] != "myapp" || cfg.Databases[1] != "otherdb" {
		t.Errorf("Databases = %v, want [myapp otherdb]", cfg.Databases)
	}
}

func TestParseConfigPasswordContainerEnv(t *testing.T) {
	raw := validRawConfig()
	delete(raw, "password_env")
	raw["container"] = "my-mariadb"
	raw["password_container_env"] = "MARIADB_ROOT_PASSWORD"

	cfg, err := ParseConfig(&inventory.PluginConfig{Config: raw})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.PasswordContainerEnv != "MARIADB_ROOT_PASSWORD" || cfg.Password != "" {
		t.Errorf("cfg = %+v, want PasswordContainerEnv set and no Password", cfg)
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]interface{})
		wantErr string
	}{
		{"missing username", func(r map[string]interface{}) { delete(r, "username") }, "username is required"},
		{"no password source", func(r map[string]interface{}) { delete(r, "password_env") }, "one of config.password_env"},
		{"unset password env", func(r map[string]interface{}) { r["password_env"] = "MARIADB_TEST_UNSET" }, "is not set"},
		{"both password sources", func(r map[string]interface{}) {
			r["container"] = "c"
			r["password_container_env"] = "X"
		}, "only one of"},
		{"container env without container", func(r map[string]interface{}) {
			delete(r, "password_env")
			r["password_container_env"] = "MARIADB_ROOT_PASSWORD"
		}, "requires config.container"},
		{"container env not a plain name", func(r map[string]interface{}) {
			delete(r, "password_env")
			r["container"] = "c"
			r["password_container_env"] = `X"; rm -rf /; "`
		}, "not a valid environment variable name"},
		{"no databases", func(r map[string]interface{}) { r["databases"] = []interface{}{} }, "at least one database"},
		{"databases not a list", func(r map[string]interface{}) { r["databases"] = "myapp" }, "expected a list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MARIADB_TEST_PASSWORD", "supersecret")
			raw := validRawConfig()
			tt.mutate(raw)
			_, err := ParseConfig(&inventory.PluginConfig{Config: raw})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseConfig() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfigNil(t *testing.T) {
	if _, err := ParseConfig(nil); err == nil {
		t.Error("ParseConfig(nil) = nil error, want error")
	}
}
