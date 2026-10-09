package controller

import (
	"context"
	"testing"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin"
)

// capturingFactory records the *inventory.PluginConfig it was called with,
// so a test can assert which config source (DB override vs inventory.yaml
// fallback) resolvePluginConfig actually picked.
func capturingFactory(captured *[]*inventory.PluginConfig) PluginFactory {
	return func(_ inventory.Server, cfg *inventory.PluginConfig, _ string) (plugin.BackupPlugin, error) {
		*captured = append(*captured, cfg)
		return &stubPlugin{}, nil
	}
}

func newResolutionController(t *testing.T, inv *inventory.Inventory) (*Controller, *metadata.Store) {
	t.Helper()
	md, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { md.Close() })
	c := New(inv, md, newStubStorage(), core.Verification{Enabled: false}, t.TempDir(), 0)
	return c, md
}

func TestResolvePluginConfigFallsBackToInventoryWhenNoDBOverride(t *testing.T) {
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"prod-db": {Plugins: map[string]*inventory.PluginConfig{
			"postgres": {Config: map[string]interface{}{"username": "inventory_user"}},
		}},
	}}
	c, _ := newResolutionController(t, inv)
	var captured []*inventory.PluginConfig
	c.RegisterPlugin("postgres", capturingFactory(&captured))

	if _, err := c.BuildPlugin(context.Background(), "prod-db", "postgres"); err != nil {
		t.Fatalf("BuildPlugin: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("factory called %d times, want 1", len(captured))
	}
	if got := captured[0].Config["username"]; got != "inventory_user" {
		t.Errorf("config.username = %v, want inventory_user (inventory.yaml fallback)", got)
	}
}

func TestResolvePluginConfigDBOverrideWinsOverInventory(t *testing.T) {
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"prod-db": {Plugins: map[string]*inventory.PluginConfig{
			"postgres": {Config: map[string]interface{}{"username": "inventory_user"}},
		}},
	}}
	c, md := newResolutionController(t, inv)
	var captured []*inventory.PluginConfig
	c.RegisterPlugin("postgres", capturingFactory(&captured))

	if err := md.SetPluginSettings(context.Background(), "prod-db", "postgres", `{"username":"db_user"}`); err != nil {
		t.Fatalf("SetPluginSettings: %v", err)
	}

	if _, err := c.BuildPlugin(context.Background(), "prod-db", "postgres"); err != nil {
		t.Fatalf("BuildPlugin: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("factory called %d times, want 1", len(captured))
	}
	if got := captured[0].Config["username"]; got != "db_user" {
		t.Errorf("config.username = %v, want db_user (a stored override must win over inventory.yaml)", got)
	}
}

// A host needs no static plugins.<name>.config block in inventory.yaml at
// all once a DB-stored override exists for it - this is the scenario the
// web-configuration feature is actually for (configuring a plugin that was
// never in inventory.yaml to begin with).
func TestResolvePluginConfigDBOverrideWorksWithNoInventoryBlockAtAll(t *testing.T) {
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"new-informix-host": {},
	}}
	c, md := newResolutionController(t, inv)
	var captured []*inventory.PluginConfig
	c.RegisterPlugin("informix", capturingFactory(&captured))

	if err := md.SetPluginSettings(context.Background(), "new-informix-host", "informix", `{"host":"10.0.0.5"}`); err != nil {
		t.Fatalf("SetPluginSettings: %v", err)
	}

	if _, err := c.BuildPlugin(context.Background(), "new-informix-host", "informix"); err != nil {
		t.Fatalf("BuildPlugin: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("factory called %d times, want 1", len(captured))
	}
	if got := captured[0].Config["host"]; got != "10.0.0.5" {
		t.Errorf("config.host = %v, want 10.0.0.5", got)
	}
}

func TestResolvePluginConfigErrorsWhenNeitherSourceHasConfig(t *testing.T) {
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{"prod-db": {}}}
	c, _ := newResolutionController(t, inv)
	var captured []*inventory.PluginConfig
	c.RegisterPlugin("postgres", capturingFactory(&captured))

	if _, err := c.BuildPlugin(context.Background(), "prod-db", "postgres"); err == nil {
		t.Fatalf("BuildPlugin: expected error, got nil")
	}
	if len(captured) != 0 {
		t.Errorf("factory called %d times, want 0 (must fail before instantiating)", len(captured))
	}
}
