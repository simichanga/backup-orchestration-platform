package filesystem

import (
	"fmt"

	"bop/internal/inventory"
)

// filesystemConfig is a server's filesystem plugin settings, parsed from
// inventory.yaml's plugins.filesystem.config block. Paths are assumed to be
// directories on the target host (see command.go's tar/untar pair, which
// wraps and strips a single top-level entry).
type filesystemConfig struct {
	Paths    []string
	Excludes []string
}

func parseConfig(cfg *inventory.PluginConfig) (filesystemConfig, error) {
	if cfg == nil || cfg.Config == nil {
		return filesystemConfig{}, fmt.Errorf("filesystem: no config provided")
	}

	paths, err := toStringSlice(cfg.Config["paths"])
	if err != nil {
		return filesystemConfig{}, fmt.Errorf("filesystem: config.paths: %w", err)
	}
	if len(paths) == 0 {
		return filesystemConfig{}, fmt.Errorf("filesystem: config.paths must list at least one path")
	}
	for _, p := range paths {
		if len(p) == 0 || p[0] != '/' {
			return filesystemConfig{}, fmt.Errorf("filesystem: config.paths: %q must be an absolute path", p)
		}
	}

	excludes, err := toStringSlice(cfg.Config["excludes"])
	if err != nil {
		return filesystemConfig{}, fmt.Errorf("filesystem: config.excludes: %w", err)
	}

	return filesystemConfig{Paths: paths, Excludes: excludes}, nil
}

func toStringSlice(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected a list, got %T", v)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string at index %d, got %T", i, item)
		}
		out[i] = s
	}
	return out, nil
}
