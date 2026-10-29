package logs

import (
	"strings"
	"testing"
	"time"

	"bop/internal/inventory"
)

func TestParseConfigValid(t *testing.T) {
	cfg, err := ParseConfig(&inventory.PluginConfig{Config: map[string]interface{}{
		"journal":            true,
		"containers":         []interface{}{"*"},
		"exclude_containers": []interface{}{"informix-*"},
		"window":             "48h",
	}})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if !cfg.Journal || len(cfg.Containers) != 1 || cfg.ExcludeContainers[0] != "informix-*" || cfg.Window != 48*time.Hour {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseConfigDefaultWindow(t *testing.T) {
	cfg, err := ParseConfig(&inventory.PluginConfig{Config: map[string]interface{}{"journal": true}})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Window != defaultWindow {
		t.Errorf("Window = %v, want %v", cfg.Window, defaultWindow)
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]interface{}
		wantErr string
	}{
		{"nothing selected", map[string]interface{}{"journal": false}, "nothing to back up"},
		{"journal not a bool", map[string]interface{}{"journal": "yes"}, "expected true or false"},
		{"containers not a list", map[string]interface{}{"containers": "*"}, "expected a list"},
		{"bad pattern", map[string]interface{}{"containers": []interface{}{"["}}, "invalid pattern"},
		{"bad window", map[string]interface{}{"journal": true, "window": "3 days"}, "positive duration"},
		{"negative window", map[string]interface{}{"journal": true, "window": "-1h"}, "positive duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig(&inventory.PluginConfig{Config: tt.raw})
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
