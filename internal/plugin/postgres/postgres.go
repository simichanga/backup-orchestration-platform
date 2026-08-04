// Package postgres implements the BackupPlugin port for PostgreSQL,
// documented as one of BOP's supported data sources. Connects to the
// target host over SSH (srv.ssh_user/ssh_key from inventory.yaml) and runs
// pg_dump there, either directly or inside a Docker container when the
// plugin's config.container is set.
package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/plugin"
	"bop/internal/sshexec"
)

const pgDumpHeaderMarker = "PostgreSQL database dump"

type Plugin struct {
	exec    sshexec.Executor
	cfg     postgresConfig
	tempDir string
}

// NewFactory returns a controller.PluginFactory-compatible factory that
// builds a postgres Plugin from a server's inventory entry and this
// plugin's config block. knownHostsFile is config.yaml's
// ssh.known_hosts_file, verified on every connection (see internal/sshexec).
func NewFactory(knownHostsFile string) func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
		pgCfg, err := parseConfig(cfg)
		if err != nil {
			return nil, err
		}
		if srv.Host == "" {
			return nil, fmt.Errorf("postgres: server host is required")
		}
		if srv.SSHUser == "" || srv.SSHKey == "" {
			return nil, fmt.Errorf("postgres: ssh_user and ssh_key are required")
		}

		return &Plugin{
			exec:    &sshexec.SSHExecutor{Addr: srv.Host + ":22", User: srv.SSHUser, KeyPath: srv.SSHKey, KnownHostsFile: knownHostsFile},
			cfg:     pgCfg,
			tempDir: tempDir,
		}, nil
	}
}

func (p *Plugin) Discover(context.Context) ([]core.Resource, error) {
	resources := make([]core.Resource, len(p.cfg.Databases))
	for i, db := range p.cfg.Databases {
		resources[i] = core.Resource{ID: db, Name: db}
	}
	return resources, nil
}

func (p *Plugin) Backup(ctx context.Context, res core.Resource) (core.Artifact, error) {
	f, err := os.CreateTemp(p.tempDir, "pg-"+res.ID+"-*")
	if err != nil {
		return core.Artifact{}, fmt.Errorf("postgres: create artifact file: %w", err)
	}
	path := f.Name()

	// Close before Remove on every error path, not after via defer: an
	// open file handle can't be removed on Windows (unlike POSIX, which
	// allows unlinking an open file), and closing before removing is
	// correct on both regardless.
	if err := p.exec.Run(ctx, dumpCommand(p.cfg, res.ID), nil, f); err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("postgres: pg_dump %s: %w", res.ID, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("postgres: stat artifact: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("postgres: close artifact: %w", err)
	}

	return core.Artifact{
		ResourceID: res.ID,
		Path:       path,
		Size:       info.Size(),
		CreatedAt:  time.Now(),
	}, nil
}

func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("postgres: open artifact: %w", err)
	}
	defer f.Close()

	if err := p.exec.Run(ctx, restoreCommand(p.cfg, a.ResourceID), f, io.Discard); err != nil {
		return fmt.Errorf("postgres: restore %s: %w", a.ResourceID, err)
	}
	return nil
}

// Verify is a cheap structural check, not a parse of the whole SQL dump:
// non-empty, and starts with pg_dump's own header comment. This is the
// "is the artifact well-formed" tier of the three-tier verification model
// (see docs/01-introduction.md), meant to catch a truncated or empty dump
// before the controller spends time checksumming and uploading it.
func (p *Plugin) Verify(ctx context.Context, a core.Artifact) error {
	info, err := os.Stat(a.Path)
	if err != nil {
		return fmt.Errorf("postgres: verify: stat artifact: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("postgres: verify: artifact is empty")
	}

	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("postgres: verify: open artifact: %w", err)
	}
	defer f.Close()

	buf := make([]byte, 4096)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("postgres: verify: read artifact: %w", err)
	}
	if !bytes.Contains(buf[:n], []byte(pgDumpHeaderMarker)) {
		return fmt.Errorf("postgres: verify: artifact does not look like pg_dump output (missing %q header)", pgDumpHeaderMarker)
	}
	return nil
}

func (p *Plugin) Health(ctx context.Context) error {
	if err := p.exec.Run(ctx, "echo ok", nil, io.Discard); err != nil {
		return fmt.Errorf("postgres: health check: %w", err)
	}
	return nil
}

func (p *Plugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "postgres", Version: "0.1.0"}
}
