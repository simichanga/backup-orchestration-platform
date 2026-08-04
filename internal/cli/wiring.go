// Package cli implements the bop command-line interface documented in
// docs/03-getting-started/*.md. Phase 1 has no daemon/API for commands to
// talk to, so each command (other than "controller") builds its own
// in-process app and runs to completion.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"bop/internal/config"
	"bop/internal/controller"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin/postgres"
	"bop/internal/storage"
)

// app bundles everything a command needs. Callers must call Close when done.
type app struct {
	Config     *config.Config
	Inventory  *inventory.Inventory
	Metadata   *metadata.Store
	Controller *controller.Controller
	Logger     *slog.Logger
}

func (a *app) Close() error {
	return a.Metadata.Close()
}

// buildApp loads config.yaml and inventory.yaml, opens the metadata store,
// constructs the storage provider and controller, and registers all
// available plugins. Only sqlite (metadata) and restic (storage) are
// implemented so far, despite both being documented as configurable -
// buildApp fails clearly on any other configured value rather than
// pretending to support it.
func buildApp(configPath string) (*app, error) {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Logging)
	slog.SetDefault(logger)

	inv, err := inventory.Load(cfg.Inventory)
	if err != nil {
		return nil, fmt.Errorf("load inventory: %w", err)
	}

	if cfg.Metadata.Driver != "sqlite" {
		return nil, fmt.Errorf("metadata.driver %q is not yet implemented (only sqlite)", cfg.Metadata.Driver)
	}
	md, err := metadata.Open(cfg.Metadata.DSN)
	if err != nil {
		return nil, fmt.Errorf("open metadata store: %w", err)
	}

	if cfg.Storage.Provider != "restic" {
		md.Close()
		return nil, fmt.Errorf("storage.provider %q is not yet implemented (only restic)", cfg.Storage.Provider)
	}
	// storage.restic.concurrency is documented but not yet enforced by
	// ResticProvider - noted here rather than silently ignored.
	sp := storage.NewResticProvider("restic", cfg.Storage.Restic.Repository, cfg.Storage.Restic.PasswordFile, cfg.Storage.Restic.ExtraArgs)

	ctl := controller.New(inv, md, sp, cfg.Verification, cfg.Controller.TempDir, cfg.Controller.JobTimeout)
	ctl.Logger = logger
	ctl.RegisterPlugin("postgres", postgres.NewFactory())

	return &app{Config: cfg, Inventory: inv, Metadata: md, Controller: ctl, Logger: logger}, nil
}

func newLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
