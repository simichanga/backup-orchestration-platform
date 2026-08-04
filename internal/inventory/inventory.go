// Package inventory loads inventory.yaml: the source of truth for hosts,
// plugin assignments, retention policies, and schedules, as documented in
// docs/01-introduction.md and docs/03-getting-started/quickstart.md.
//
// Unlike config.yaml, inventory.yaml has no documented environment-variable
// overlay, so this package parses it directly with yaml.v3 instead of
// going through Viper. Viper's internal map merging silently drops YAML
// keys with a null value (e.g. a plugin with no config, like `docker:`),
// which is exactly the shape this schema relies on - yaml.v3 preserves them.
package inventory

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"bop/internal/core"
)

type Inventory struct {
	Servers map[string]Server `yaml:"servers"`
}

// Server is one host's inventory entry. Plugins is a map, not a list: a
// plugin with no configuration (e.g. docker:) has a nil value, and a plugin
// that needs configuration (e.g. postgres) nests it under a config key.
type Server struct {
	Host         string                   `yaml:"host"`
	SSHUser      string                   `yaml:"ssh_user"`
	SSHKey       string                   `yaml:"ssh_key"`
	Plugins      map[string]*PluginConfig `yaml:"plugins"`
	Retention    core.Policy              `yaml:"retention"`
	Schedule     string                   `yaml:"schedule"`
	Verification *core.Verification       `yaml:"verification"`
}

// PluginConfig holds a plugin's host-specific settings. Contents are
// plugin-specific (e.g. postgres needs username/password_env/databases) so
// they are decoded generically here; each plugin implementation parses its
// own config out of this map.
type PluginConfig struct {
	Config map[string]interface{} `yaml:"config"`
}

// Load reads and validates inventory.yaml from path.
func Load(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("inventory: read %s: %w", path, err)
	}

	var inv Inventory
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("inventory: parse %s: %w", path, err)
	}

	if err := inv.Validate(); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}

	return &inv, nil
}

// Validate checks structural constraints. Cron syntax validation of
// Schedule is deferred to the scheduler, which owns the cron library.
func (inv *Inventory) Validate() error {
	if len(inv.Servers) == 0 {
		return fmt.Errorf("no servers defined")
	}
	for name, srv := range inv.Servers {
		if srv.Host == "" {
			return fmt.Errorf("server %q: host is required", name)
		}
		if len(srv.Plugins) == 0 {
			return fmt.Errorf("server %q: at least one plugin is required", name)
		}
		if srv.Retention.Daily < 0 || srv.Retention.Weekly < 0 || srv.Retention.Monthly < 0 || srv.Retention.Yearly < 0 {
			return fmt.Errorf("server %q: retention values must not be negative", name)
		}
	}
	return nil
}
