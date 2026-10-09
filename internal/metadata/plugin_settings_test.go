package metadata

import (
	"context"
	"testing"
)

func TestGetPluginSettingsNotFound(t *testing.T) {
	s := openTestStore(t)

	_, found, err := s.GetPluginSettings(context.Background(), "prod-db", "informix")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if found {
		t.Errorf("found = true, want false (nothing stored yet)")
	}
}

func TestSetAndGetPluginSettings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SetPluginSettings(ctx, "prod-db", "informix", `{"host":"10.0.0.5"}`); err != nil {
		t.Fatalf("SetPluginSettings: %v", err)
	}

	got, found, err := s.GetPluginSettings(ctx, "prod-db", "informix")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got != `{"host":"10.0.0.5"}` {
		t.Errorf("config_json = %q, want %q", got, `{"host":"10.0.0.5"}`)
	}
}

func TestSetPluginSettingsOverwritesExisting(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SetPluginSettings(ctx, "prod-db", "informix", `{"host":"10.0.0.5"}`); err != nil {
		t.Fatalf("SetPluginSettings (first): %v", err)
	}
	if err := s.SetPluginSettings(ctx, "prod-db", "informix", `{"host":"10.0.0.9"}`); err != nil {
		t.Fatalf("SetPluginSettings (second): %v", err)
	}

	got, _, err := s.GetPluginSettings(ctx, "prod-db", "informix")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if got != `{"host":"10.0.0.9"}` {
		t.Errorf("config_json = %q, want the overwritten value %q", got, `{"host":"10.0.0.9"}`)
	}
}

func TestPluginSettingsScopedByHostAndPlugin(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SetPluginSettings(ctx, "host-a", "informix", `{"host":"a"}`); err != nil {
		t.Fatalf("SetPluginSettings(host-a): %v", err)
	}
	if err := s.SetPluginSettings(ctx, "host-b", "informix", `{"host":"b"}`); err != nil {
		t.Fatalf("SetPluginSettings(host-b): %v", err)
	}
	if err := s.SetPluginSettings(ctx, "host-a", "postgres", `{"host":"a-pg"}`); err != nil {
		t.Fatalf("SetPluginSettings(host-a, postgres): %v", err)
	}

	got, _, err := s.GetPluginSettings(ctx, "host-a", "informix")
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if got != `{"host":"a"}` {
		t.Errorf("host-a/informix = %q, want unaffected by host-b or host-a/postgres", got)
	}
}
