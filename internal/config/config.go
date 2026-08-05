// Package config loads BOP's main configuration (config.yaml), as documented
// in docs/03-getting-started/configuration.md. Precedence, lowest to
// highest: config file, environment variables (BOP_ prefixed), CLI flags.
// CLI flag binding is not wired up here yet since the Cobra layer doesn't
// exist; Load accepts a pre-built *viper.Viper so a future CLI layer can
// bind pflags into it before calling Load.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"bop/internal/core"
)

type Config struct {
	Inventory    string            `mapstructure:"inventory"`
	Storage      StorageConfig     `mapstructure:"storage"`
	Controller   ControllerConfig  `mapstructure:"controller"`
	Scheduler    SchedulerConfig   `mapstructure:"scheduler"`
	SSH          SSHConfig         `mapstructure:"ssh"`
	Metadata     MetadataConfig    `mapstructure:"metadata"`
	API          APIConfig         `mapstructure:"api"`
	Metrics      MetricsConfig     `mapstructure:"metrics"`
	Logging      LoggingConfig     `mapstructure:"logging"`
	Verification core.Verification `mapstructure:"verification"`
	Plugins      PluginsConfig     `mapstructure:"plugins"`
	Secrets      SecretsConfig     `mapstructure:"secrets"`
}

type StorageConfig struct {
	Provider string       `mapstructure:"provider"`
	Restic   ResticConfig `mapstructure:"restic"`
}

type ResticConfig struct {
	Repository string `mapstructure:"repository"`
	// Exactly one of PasswordFile or PasswordEnv is required: the same
	// file-or-env choice postgres's password_env already offers, so every
	// secret BOP touches supports the same two operator-side delivery
	// mechanisms (a systemd LoadCredential= path, or an EnvironmentFile=
	// -injected variable) rather than restic being the odd one out with
	// only a file option.
	PasswordFile string   `mapstructure:"password_file"`
	PasswordEnv  string   `mapstructure:"password_env"`
	ExtraArgs    []string `mapstructure:"extra_args"`
	Concurrency  int      `mapstructure:"concurrency"`
}

type ControllerConfig struct {
	Concurrency int           `mapstructure:"concurrency"`
	JobTimeout  time.Duration `mapstructure:"job_timeout"`
	TempDir     string        `mapstructure:"temp_dir"`
}

type SchedulerConfig struct {
	CronLocation string `mapstructure:"cron_location"`
}

// SSHConfig governs how plugin.SSH-based plugins (postgres, filesystem)
// verify the hosts they connect to. There is no "insecure" toggle: every
// connection is checked against KnownHostsFile (see internal/sshexec).
type SSHConfig struct {
	KnownHostsFile string `mapstructure:"known_hosts_file"`
}

type MetadataConfig struct {
	Driver string `mapstructure:"driver"`
	DSN    string `mapstructure:"dsn"`
	// EventRetention bounds the events table (see internal/metadata's
	// EventPublisher): events fire many times per job, unlike jobs/
	// snapshots (one row per job, never pruned), so leaving this table
	// unbounded would grow it far faster than the rest of the metadata
	// store.
	EventRetention time.Duration `mapstructure:"event_retention"`
}

// APIConfig governs the optional read-only HTTP API "bop controller" can
// serve alongside metrics, gated by Enabled (off by default - most Phase 1
// deployments don't need it, the CLI already covers Phase 1's needs). v1 is
// REST-only; the docs/02-architecture.md gRPC+REST-gateway design is
// deferred until something actually needs gRPC, so there is no grpc_addr
// here. Every request must carry a bearer token from TokensFile/TokenEnv -
// there is no anonymous access, matching SSH's no-insecure-fallback
// posture elsewhere in BOP.
type APIConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	Addr       string `mapstructure:"addr"`
	TokensFile string `mapstructure:"tokens_file"`
	TokenEnv   string `mapstructure:"token_env"`
}

type MetricsConfig struct {
	Port int    `mapstructure:"port"`
	Path string `mapstructure:"path"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type PluginsConfig struct {
	Dir           string `mapstructure:"dir"`
	AllowUnsigned bool   `mapstructure:"allow_unsigned"`
}

type SecretsConfig struct {
	EnvFile string `mapstructure:"env_file"`
}

var validLogLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
var validLogFormats = map[string]bool{"json": true, "text": true}
var validMetadataDrivers = map[string]bool{"sqlite": true, "postgres": true}
var validStorageProviders = map[string]bool{"restic": true}

func setDefaults(v *viper.Viper) {
	v.SetDefault("inventory", "/etc/bop/inventory.yaml")

	v.SetDefault("storage.provider", "restic")
	v.SetDefault("storage.restic.concurrency", 2)

	v.SetDefault("controller.concurrency", 4)
	v.SetDefault("controller.job_timeout", "2h")
	v.SetDefault("controller.temp_dir", "/tmp/bop")

	v.SetDefault("scheduler.cron_location", "Local")

	v.SetDefault("ssh.known_hosts_file", "/etc/bop/known_hosts")

	v.SetDefault("metadata.driver", "sqlite")
	v.SetDefault("metadata.dsn", "/var/lib/bop/metadata.db")
	v.SetDefault("metadata.event_retention", "720h") // 30 days

	v.SetDefault("api.enabled", false)
	v.SetDefault("api.addr", ":9091")

	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	v.SetDefault("verification.enabled", false)
	v.SetDefault("verification.target_dir", "/tmp/bop-verify")

	v.SetDefault("plugins.dir", "/usr/local/lib/bop/plugins")
	v.SetDefault("plugins.allow_unsigned", false)

	v.SetDefault("secrets.env_file", "/etc/bop/secrets.env")
}

// Load reads config.yaml from path, applies BOP_ prefixed environment
// variable overrides, and returns a validated Config. Pass a *viper.Viper
// with CLI flags already bound (via BindPFlag) to let flags take highest
// precedence; pass nil to use a fresh instance with just file and env.
func Load(path string, v *viper.Viper) (*Config, error) {
	if v == nil {
		v = viper.New()
	}

	setDefaults(v)

	v.SetEnvPrefix("BOP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return &cfg, nil
}

// Validate checks structural constraints that a type-correct but
// nonsensical config could still violate (e.g. an unsupported log level).
func (c *Config) Validate() error {
	if !validStorageProviders[c.Storage.Provider] {
		return fmt.Errorf("storage.provider: unsupported value %q", c.Storage.Provider)
	}
	if c.Storage.Provider == "restic" {
		if c.Storage.Restic.Repository == "" {
			return fmt.Errorf("storage.restic.repository: required when storage.provider is restic")
		}
		hasFile := c.Storage.Restic.PasswordFile != ""
		hasEnv := c.Storage.Restic.PasswordEnv != ""
		if hasFile && hasEnv {
			return fmt.Errorf("storage.restic: password_file and password_env are mutually exclusive")
		}
		if !hasFile && !hasEnv {
			return fmt.Errorf("storage.restic: one of password_file or password_env is required")
		}
	}
	if c.API.Enabled {
		hasFile := c.API.TokensFile != ""
		hasEnv := c.API.TokenEnv != ""
		if hasFile && hasEnv {
			return fmt.Errorf("api: tokens_file and token_env are mutually exclusive")
		}
		if !hasFile && !hasEnv {
			return fmt.Errorf("api: one of tokens_file or token_env is required when api.enabled is true")
		}
	}
	if c.Controller.Concurrency < 1 {
		return fmt.Errorf("controller.concurrency: must be at least 1, got %d", c.Controller.Concurrency)
	}
	if !validMetadataDrivers[c.Metadata.Driver] {
		return fmt.Errorf("metadata.driver: unsupported value %q", c.Metadata.Driver)
	}
	if c.Metadata.EventRetention <= 0 {
		return fmt.Errorf("metadata.event_retention: must be positive, got %v", c.Metadata.EventRetention)
	}
	if !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level: unsupported value %q", c.Logging.Level)
	}
	if !validLogFormats[c.Logging.Format] {
		return fmt.Errorf("logging.format: unsupported value %q", c.Logging.Format)
	}
	return nil
}
