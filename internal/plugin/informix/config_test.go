package informix

import (
	"testing"

	"bop/internal/inventory"
)

func validRawConfig() map[string]interface{} {
	return map[string]interface{}{
		"host":         "127.0.0.1",
		"port":         9088,
		"username":     "informix",
		"password_env": "IFX_TEST_PASSWORD",
		"server_name":  "informix",
		"database":     "sysmaster",
		"tables":       []interface{}{"customers", "orders"},
		"agent_path":   "/usr/local/bin/ifx-agent",
	}
}

func TestParseConfigValid(t *testing.T) {
	t.Setenv("IFX_TEST_PASSWORD", "supersecret")

	cfg, err := ParseConfig(&inventory.PluginConfig{Config: validRawConfig()})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 9088 {
		t.Errorf("Port = %d, want 9088", cfg.Port)
	}
	if cfg.Username != "informix" {
		t.Errorf("Username = %q, want informix", cfg.Username)
	}
	if cfg.Password != "supersecret" {
		t.Errorf("Password = %q, want supersecret", cfg.Password)
	}
	if cfg.PasswordEnv != "IFX_TEST_PASSWORD" {
		t.Errorf("PasswordEnv = %q, want IFX_TEST_PASSWORD", cfg.PasswordEnv)
	}
	if cfg.ServerName != "informix" {
		t.Errorf("ServerName = %q, want informix", cfg.ServerName)
	}
	if cfg.Database != "sysmaster" {
		t.Errorf("Database = %q, want sysmaster", cfg.Database)
	}
	if len(cfg.Tables) != 2 || cfg.Tables[0] != "customers" || cfg.Tables[1] != "orders" {
		t.Errorf("Tables = %v, want [customers orders]", cfg.Tables)
	}
	if cfg.AgentPath != "/usr/local/bin/ifx-agent" {
		t.Errorf("AgentPath = %q, want /usr/local/bin/ifx-agent", cfg.AgentPath)
	}
}

// Port arrives as float64 when the config map is decoded from JSON (the
// web-configuration API path) instead of yaml.v3's int - ParseConfig must
// accept both.
func TestParseConfigPortFromJSONNumber(t *testing.T) {
	t.Setenv("IFX_TEST_PASSWORD", "supersecret")
	raw := validRawConfig()
	raw["port"] = float64(9088)

	cfg, err := ParseConfig(&inventory.PluginConfig{Config: raw})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Port != 9088 {
		t.Errorf("Port = %d, want 9088", cfg.Port)
	}
}

func TestParseConfigQualifiedTableName(t *testing.T) {
	t.Setenv("IFX_TEST_PASSWORD", "supersecret")
	raw := validRawConfig()
	raw["tables"] = []interface{}{"informix.customers"}

	cfg, err := ParseConfig(&inventory.PluginConfig{Config: raw})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if len(cfg.Tables) != 1 || cfg.Tables[0] != "informix.customers" {
		t.Errorf("Tables = %v, want [informix.customers]", cfg.Tables)
	}
}

func TestParseConfigErrors(t *testing.T) {
	t.Setenv("IFX_TEST_PASSWORD", "supersecret")

	tests := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{"missing host", func(m map[string]interface{}) { delete(m, "host") }},
		{"missing port", func(m map[string]interface{}) { delete(m, "port") }},
		{"port wrong type", func(m map[string]interface{}) { m["port"] = []interface{}{1} }},
		{"missing username", func(m map[string]interface{}) { delete(m, "username") }},
		{"missing password_env", func(m map[string]interface{}) { delete(m, "password_env") }},
		{"password_env not set", func(m map[string]interface{}) { m["password_env"] = "IFX_TEST_UNSET_VAR" }},
		{"missing server_name", func(m map[string]interface{}) { delete(m, "server_name") }},
		{"missing database", func(m map[string]interface{}) { delete(m, "database") }},
		{"missing tables", func(m map[string]interface{}) { delete(m, "tables") }},
		{"tables wrong type", func(m map[string]interface{}) { m["tables"] = "customers" }},
		{"tables element wrong type", func(m map[string]interface{}) { m["tables"] = []interface{}{1} }},
		{"empty tables list", func(m map[string]interface{}) { m["tables"] = []interface{}{} }},
		{"table name with injection attempt", func(m map[string]interface{}) { m["tables"] = []interface{}{"x; drop table y"} }},
		{"table name with too many qualifiers", func(m map[string]interface{}) { m["tables"] = []interface{}{"a.b.c"} }},
		{"missing agent_path", func(m map[string]interface{}) { delete(m, "agent_path") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := validRawConfig()
			tt.mutate(raw)
			if _, err := ParseConfig(&inventory.PluginConfig{Config: raw}); err == nil {
				t.Fatalf("ParseConfig: expected error, got nil")
			}
		})
	}
}

func TestParseConfigNilConfig(t *testing.T) {
	if _, err := ParseConfig(nil); err == nil {
		t.Fatalf("ParseConfig(nil): expected error, got nil")
	}
}
