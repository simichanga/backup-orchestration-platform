package informix

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"bop/internal/inventory"
)

// validTableName rejects anything that isn't a plain (optionally
// owner-qualified) SQL identifier. Table names come from admin-supplied
// config, not live request input, but they're interpolated directly into
// SQL text in Backup/Restore (ExecuteQuery has no identifier-quoting
// parameter, only value binds), so this is validated once here rather than
// trusted at every call site that builds a query string.
var validTableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// Config is the informix plugin's parsed configuration. Exported (unlike
// postgres's lowercase postgresConfig) because the web-configuration API
// handler (internal/api) validates a submitted config through ParseConfig
// before persisting it - the same validation the inventory.yaml path goes
// through, just fed a JSON-decoded map instead of a yaml.v3-decoded one.
// Both produce the same dynamic shape (map[string]interface{}, []interface{},
// string), so one parser serves both sources.
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string // resolved from PasswordEnv, never stored/transmitted directly
	PasswordEnv string
	ServerName  string // Informix INFORMIXSERVER
	Database    string
	// Tables lists the tables to back up, one core.Resource per entry.
	Tables []string
	// AgentPath is the path to the locally-available ifx-agent binary this
	// plugin spawns as a subprocess - see agentclient.go. Unlike postgres's
	// Container field, this isn't optional: there is no other way to talk
	// to Informix here, there's no SSH/remote-exec fallback.
	AgentPath string
}

// ParseConfig validates and extracts an informix Config from the generic
// per-plugin config map - cfg.Config for the inventory.yaml path, or a
// JSON-decoded map for the web-configuration API path. Follows the same
// conventions as postgres/config.go: fail clearly on a missing/malformed
// required field, never accept a plaintext password (only password_env,
// resolved via os.Getenv).
func ParseConfig(cfg *inventory.PluginConfig) (Config, error) {
	if cfg == nil || cfg.Config == nil {
		return Config{}, fmt.Errorf("informix: no config provided")
	}

	host, _ := cfg.Config["host"].(string)
	if host == "" {
		return Config{}, fmt.Errorf("informix: config.host is required")
	}

	port, err := toInt(cfg.Config["port"])
	if err != nil {
		return Config{}, fmt.Errorf("informix: config.port: %w", err)
	}
	if port == 0 {
		return Config{}, fmt.Errorf("informix: config.port is required")
	}

	username, _ := cfg.Config["username"].(string)
	if username == "" {
		return Config{}, fmt.Errorf("informix: config.username is required")
	}

	passwordEnv, _ := cfg.Config["password_env"].(string)
	if passwordEnv == "" {
		return Config{}, fmt.Errorf("informix: config.password_env is required")
	}
	password := os.Getenv(passwordEnv)
	if password == "" {
		return Config{}, fmt.Errorf("informix: environment variable %q (password_env) is not set", passwordEnv)
	}

	serverName, _ := cfg.Config["server_name"].(string)
	if serverName == "" {
		return Config{}, fmt.Errorf("informix: config.server_name is required")
	}

	database, _ := cfg.Config["database"].(string)
	if database == "" {
		return Config{}, fmt.Errorf("informix: config.database is required")
	}

	tables, err := toStringSlice(cfg.Config["tables"])
	if err != nil {
		return Config{}, fmt.Errorf("informix: config.tables: %w", err)
	}
	if len(tables) == 0 {
		return Config{}, fmt.Errorf("informix: config.tables must list at least one table")
	}
	for _, t := range tables {
		if !validTableName.MatchString(t) {
			return Config{}, fmt.Errorf("informix: config.tables: %q is not a plain [owner.]table identifier", t)
		}
	}

	agentPath, _ := cfg.Config["agent_path"].(string)
	if agentPath == "" {
		return Config{}, fmt.Errorf("informix: config.agent_path is required")
	}

	return Config{
		Host:        host,
		Port:        port,
		Username:    username,
		Password:    password,
		PasswordEnv: passwordEnv,
		ServerName:  serverName,
		Database:    database,
		Tables:      tables,
		AgentPath:   agentPath,
	}, nil
}

// toInt accepts the two shapes a port number realistically arrives in:
// yaml.v3 decodes an unquoted YAML integer as int, while encoding/json
// always decodes a JSON number as float64 - both are accepted so the same
// ParseConfig serves the inventory.yaml and web-API paths without the
// caller needing to normalize first.
func toInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case nil:
		return 0, nil
	case int:
		return n, nil
	case float64:
		return int(n), nil
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("expected a number, got %q", n)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}

// toStringSlice mirrors postgres/config.go's helper of the same name - kept
// as its own small copy per docs/04-writing-plugins.md's documented
// convention (duplicating a few lines per plugin, not worth sharing).
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
