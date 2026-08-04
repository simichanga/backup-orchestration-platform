package postgres

import (
	"fmt"
	"os"

	"bop/internal/inventory"
)

type postgresConfig struct {
	Username  string
	Password  string
	Databases []string
	// Container, when set, is the name of the Docker container running
	// Postgres on the target host; pg_dump/psql run inside it via
	// `docker exec` instead of directly on the host. Most Postgres
	// instances in this deployment are containerized.
	Container string
}

func parseConfig(cfg *inventory.PluginConfig) (postgresConfig, error) {
	if cfg == nil || cfg.Config == nil {
		return postgresConfig{}, fmt.Errorf("postgres: no config provided")
	}

	username, _ := cfg.Config["username"].(string)
	if username == "" {
		return postgresConfig{}, fmt.Errorf("postgres: config.username is required")
	}

	passwordEnv, _ := cfg.Config["password_env"].(string)
	if passwordEnv == "" {
		return postgresConfig{}, fmt.Errorf("postgres: config.password_env is required")
	}
	password := os.Getenv(passwordEnv)
	if password == "" {
		return postgresConfig{}, fmt.Errorf("postgres: environment variable %q (password_env) is not set", passwordEnv)
	}

	databases, err := toStringSlice(cfg.Config["databases"])
	if err != nil {
		return postgresConfig{}, fmt.Errorf("postgres: config.databases: %w", err)
	}
	if len(databases) == 0 {
		return postgresConfig{}, fmt.Errorf("postgres: config.databases must list at least one database")
	}

	container, _ := cfg.Config["container"].(string)

	return postgresConfig{
		Username:  username,
		Password:  password,
		Databases: databases,
		Container: container,
	}, nil
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
