package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetPluginSettings returns the stored config_json for (host, plugin), and
// whether anything is stored at all - distinct from an empty string, so a
// caller (Controller.resolvePluginConfig) can tell "no override, fall back
// to inventory.yaml" apart from "an override exists but is somehow empty".
func (s *Store) GetPluginSettings(ctx context.Context, host, plugin string) (string, bool, error) {
	row := s.queryRow(ctx, `SELECT config_json FROM plugin_settings WHERE host = ? AND plugin = ?`, host, plugin)
	var configJSON string
	if err := row.Scan(&configJSON); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("metadata: get plugin settings for %s/%s: %w", host, plugin, err)
	}
	return configJSON, true, nil
}

// SetPluginSettings persists (or replaces) the config for (host, plugin) -
// takes effect on the next job that resolves this plugin's config, with no
// controller restart (see Controller.resolvePluginConfig).
func (s *Store) SetPluginSettings(ctx context.Context, host, plugin, configJSON string) error {
	_, err := s.exec(ctx, `
		INSERT INTO plugin_settings (host, plugin, config_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (host, plugin) DO UPDATE SET config_json = excluded.config_json, updated_at = excluded.updated_at`,
		host, plugin, configJSON, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("metadata: set plugin settings for %s/%s: %w", host, plugin, err)
	}
	return nil
}
