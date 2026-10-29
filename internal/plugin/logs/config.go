package logs

import (
	"fmt"
	"path"
	"time"

	"bop/internal/inventory"
)

// defaultWindow covers two missed nightly runs. Consecutive daily
// snapshots overlap by most of the window, which restic's
// content-defined chunking deduplicates, so a wider window costs dump
// time and temp disk, not repository space.
const defaultWindow = 72 * time.Hour

type Config struct {
	// Journal backs up the systemd journal (which is also where
	// rsyslog's /var/log/syslog comes from). Needs the SSH user to be in
	// the systemd-journal (or adm) group.
	Journal bool
	// Containers are path.Match-style patterns of Docker container names
	// whose logs to back up; "*" means every container, running or not.
	// Matched against `docker ps -a` on every run, so containers created
	// later are picked up without an inventory change. Needs the SSH user
	// to be able to run docker.
	Containers []string
	// ExcludeContainers removes names matched by Containers.
	ExcludeContainers []string
	// Window is how far back each run reaches.
	Window time.Duration
}

func ParseConfig(cfg *inventory.PluginConfig) (Config, error) {
	if cfg == nil || cfg.Config == nil {
		return Config{}, fmt.Errorf("logs: no config provided")
	}

	var c Config
	if v, ok := cfg.Config["journal"]; ok {
		b, ok := v.(bool)
		if !ok {
			return Config{}, fmt.Errorf("logs: config.journal: expected true or false, got %T", v)
		}
		c.Journal = b
	}

	var err error
	if c.Containers, err = patterns(cfg.Config["containers"]); err != nil {
		return Config{}, fmt.Errorf("logs: config.containers: %w", err)
	}
	if c.ExcludeContainers, err = patterns(cfg.Config["exclude_containers"]); err != nil {
		return Config{}, fmt.Errorf("logs: config.exclude_containers: %w", err)
	}
	if !c.Journal && len(c.Containers) == 0 {
		return Config{}, fmt.Errorf("logs: nothing to back up - set config.journal: true and/or config.containers")
	}

	c.Window = defaultWindow
	if v, ok := cfg.Config["window"]; ok {
		s, ok := v.(string)
		if !ok {
			return Config{}, fmt.Errorf("logs: config.window: expected a duration string like \"72h\", got %T", v)
		}
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("logs: config.window: %q is not a positive duration like \"72h\"", s)
		}
		c.Window = d
	}
	return c, nil
}

// patterns reads a list of container-name patterns, rejecting malformed
// ones up front rather than letting path.Match fail at backup time.
func patterns(v interface{}) ([]string, error) {
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
		if !ok || s == "" {
			return nil, fmt.Errorf("expected a non-empty string at index %d", i)
		}
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", s, err)
		}
		out[i] = s
	}
	return out, nil
}
