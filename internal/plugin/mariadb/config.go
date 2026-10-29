package mariadb

import (
	"fmt"
	"os"
	"regexp"

	"bop/internal/inventory"
)

type Config struct {
	Username  string
	Databases []string
	// Container, when set, is the name of the Docker container running
	// MariaDB on the target host; mariadb-dump/mariadb run inside it via
	// `docker exec` instead of directly on the host.
	Container string
	// Exactly one of Password/PasswordContainerEnv is set.
	//
	// Password is read on the controller from config.password_env, the
	// same convention the postgres plugin uses.
	Password string
	// PasswordContainerEnv names an environment variable *inside*
	// Container (e.g. the official image's MARIADB_ROOT_PASSWORD) that
	// holds the password. The secret is then never copied to the
	// controller or onto any command line - it is expanded by a shell
	// inside the container straight into MYSQL_PWD. Requires Container.
	PasswordContainerEnv string
}

// envVarName restricts PasswordContainerEnv to a plain POSIX variable
// name: it is interpolated into an `sh -c` script (see execPrefix), so
// anything else is rejected rather than quoted.
var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ParseConfig(cfg *inventory.PluginConfig) (Config, error) {
	if cfg == nil || cfg.Config == nil {
		return Config{}, fmt.Errorf("mariadb: no config provided")
	}

	username, _ := cfg.Config["username"].(string)
	if username == "" {
		return Config{}, fmt.Errorf("mariadb: config.username is required")
	}

	container, _ := cfg.Config["container"].(string)

	passwordEnv, _ := cfg.Config["password_env"].(string)
	passwordContainerEnv, _ := cfg.Config["password_container_env"].(string)

	var password string
	switch {
	case passwordEnv != "" && passwordContainerEnv != "":
		return Config{}, fmt.Errorf("mariadb: set only one of config.password_env and config.password_container_env")
	case passwordEnv != "":
		password = os.Getenv(passwordEnv)
		if password == "" {
			return Config{}, fmt.Errorf("mariadb: environment variable %q (password_env) is not set", passwordEnv)
		}
	case passwordContainerEnv != "":
		if container == "" {
			return Config{}, fmt.Errorf("mariadb: config.password_container_env requires config.container")
		}
		if !envVarName.MatchString(passwordContainerEnv) {
			return Config{}, fmt.Errorf("mariadb: config.password_container_env %q is not a valid environment variable name", passwordContainerEnv)
		}
	default:
		return Config{}, fmt.Errorf("mariadb: one of config.password_env or config.password_container_env is required")
	}

	databases, err := toStringSlice(cfg.Config["databases"])
	if err != nil {
		return Config{}, fmt.Errorf("mariadb: config.databases: %w", err)
	}
	if len(databases) == 0 {
		return Config{}, fmt.Errorf("mariadb: config.databases must list at least one database")
	}

	return Config{
		Username:             username,
		Databases:            databases,
		Container:            container,
		Password:             password,
		PasswordContainerEnv: passwordContainerEnv,
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
