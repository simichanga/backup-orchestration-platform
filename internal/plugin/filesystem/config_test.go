package filesystem

import (
	"testing"

	"bop/internal/inventory"
)

func validRawConfig() map[string]interface{} {
	return map[string]interface{}{
		"paths": []interface{}{"/var/www", "/etc/myapp"},
	}
}

func TestParseConfigValid(t *testing.T) {
	cfg, err := parseConfig(&inventory.PluginConfig{Config: validRawConfig()})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Paths) != 2 || cfg.Paths[0] != "/var/www" || cfg.Paths[1] != "/etc/myapp" {
		t.Errorf("Paths = %v, want [/var/www /etc/myapp]", cfg.Paths)
	}
	if len(cfg.Excludes) != 0 {
		t.Errorf("Excludes = %v, want empty (not configured)", cfg.Excludes)
	}
}

func TestParseConfigWithExcludes(t *testing.T) {
	raw := validRawConfig()
	raw["excludes"] = []interface{}{"*.log", "node_modules"}

	cfg, err := parseConfig(&inventory.PluginConfig{Config: raw})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Excludes) != 2 || cfg.Excludes[0] != "*.log" || cfg.Excludes[1] != "node_modules" {
		t.Errorf("Excludes = %v, want [*.log node_modules]", cfg.Excludes)
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{"missing paths", func(m map[string]interface{}) { delete(m, "paths") }},
		{"paths wrong type", func(m map[string]interface{}) { m["paths"] = "/var/www" }},
		{"paths element wrong type", func(m map[string]interface{}) { m["paths"] = []interface{}{1} }},
		{"empty paths list", func(m map[string]interface{}) { m["paths"] = []interface{}{} }},
		{"relative path", func(m map[string]interface{}) { m["paths"] = []interface{}{"var/www"} }},
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
