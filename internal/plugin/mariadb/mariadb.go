// Package mariadb implements the BackupPlugin port for MariaDB (and
// MySQL-compatible servers shipping mariadb-dump). Connects to the target
// host over SSH (srv.ssh_user/ssh_key from inventory.yaml) and runs
// mariadb-dump there, either directly or inside a Docker container when
// the plugin's config.container is set. Structurally a sibling of the
// postgres plugin.
package mariadb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/plugin"
	"bop/internal/sshexec"
)

const (
	dumpHeaderMarker = "MariaDB dump"
	// dumpTrailerMarker is the comment mariadb-dump writes as its very
	// last line on success; its absence means a truncated dump.
	dumpTrailerMarker = "-- Dump completed"
)

type Plugin struct {
	exec    sshexec.Executor
	cfg     Config
	tempDir string
}

// NewFactory returns a controller.PluginFactory-compatible factory that
// builds a mariadb Plugin from a server's inventory entry and this
// plugin's config block. knownHostsFile is config.yaml's
// ssh.known_hosts_file, verified on every connection (see internal/sshexec).
func NewFactory(knownHostsFile string) func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
		mCfg, err := ParseConfig(cfg)
		if err != nil {
			return nil, err
		}
		if srv.Host == "" {
			return nil, fmt.Errorf("mariadb: server host is required")
		}
		if srv.SSHUser == "" || srv.SSHKey == "" {
			return nil, fmt.Errorf("mariadb: ssh_user and ssh_key are required")
		}

		return &Plugin{
			exec:    &sshexec.SSHExecutor{Addr: srv.Host + ":22", User: srv.SSHUser, KeyPath: srv.SSHKey, KnownHostsFile: knownHostsFile},
			cfg:     mCfg,
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
	f, err := os.CreateTemp(p.tempDir, "mariadb-"+res.ID+"-*")
	if err != nil {
		return core.Artifact{}, fmt.Errorf("mariadb: create artifact file: %w", err)
	}
	path := f.Name()

	// Close before Remove on every error path - see the postgres plugin's
	// identical note (Windows can't remove an open file).
	if err := p.exec.Run(ctx, dumpCommand(p.cfg, res.ID), nil, f); err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("mariadb: mariadb-dump %s: %w", res.ID, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("mariadb: stat artifact: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("mariadb: close artifact: %w", err)
	}

	return core.Artifact{
		ResourceID: res.ID,
		Path:       path,
		Size:       info.Size(),
		CreatedAt:  time.Now(),
	}, nil
}

// Restore restores a.Path's dump into a.ResourceID. For a restore-test
// (a.ResourceID carries plugin.RestoreTestSuffix) it provisions a scratch
// database first and drops it afterward, best-effort, and refuses to touch
// one that already existed - same semantics as the postgres plugin.
func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("mariadb: open artifact: %w", err)
	}
	defer f.Close()

	isRestoreTest := strings.HasSuffix(a.ResourceID, plugin.RestoreTestSuffix)
	if isRestoreTest {
		if err := p.exec.Run(ctx, createDatabaseCommand(p.cfg, a.ResourceID), nil, io.Discard); err != nil {
			if isAlreadyExistsError(err) {
				return fmt.Errorf("mariadb: restore-test scratch database %q already exists - not created by this run, refusing to restore into or drop it; a previous restore-test may have crashed before cleanup, drop it manually to retry: %w", a.ResourceID, err)
			}
			return fmt.Errorf("mariadb: create restore-test scratch database %q: %w", a.ResourceID, err)
		}
	}

	if err := p.exec.Run(ctx, restoreCommand(p.cfg, a.ResourceID), f, io.Discard); err != nil {
		if isRestoreTest {
			p.dropScratchDatabase(ctx, a.ResourceID)
		}
		return fmt.Errorf("mariadb: restore %s: %w", a.ResourceID, err)
	}

	if isRestoreTest {
		p.dropScratchDatabase(ctx, a.ResourceID)
	}
	return nil
}

// dropScratchDatabase tears down a restore-test's scratch database,
// best-effort, mirroring the postgres plugin.
func (p *Plugin) dropScratchDatabase(ctx context.Context, database string) {
	if err := p.exec.Run(ctx, dropDatabaseCommand(p.cfg, database), nil, io.Discard); err != nil {
		slog.Default().Warn("mariadb: restore-test scratch database cleanup failed", "database", database, "error", err)
	}
}

// isAlreadyExistsError reports whether err is CREATE DATABASE's
// ER_DB_CREATE_EXISTS failure ("Can't create database 'x'; database
// exists"), matched by substring since the executor carries stderr as
// plain text.
func isAlreadyExistsError(err error) bool {
	return strings.Contains(err.Error(), "database exists")
}

// Verify is a cheap structural check: non-empty, mariadb-dump's header
// comment near the start, and its "Dump completed" trailer at the end.
// The trailer check catches a dump cut short (e.g. the server or SSH
// connection dropping mid-stream) that still has a valid header.
func (p *Plugin) Verify(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("mariadb: verify: open artifact: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("mariadb: verify: stat artifact: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("mariadb: verify: artifact is empty")
	}

	const window = 4096
	head := make([]byte, window)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("mariadb: verify: read artifact: %w", err)
	}
	if !bytes.Contains(head[:n], []byte(dumpHeaderMarker)) {
		return fmt.Errorf("mariadb: verify: artifact does not look like mariadb-dump output (missing %q header)", dumpHeaderMarker)
	}

	tailStart := info.Size() - window
	if tailStart < 0 {
		tailStart = 0
	}
	tail := make([]byte, info.Size()-tailStart)
	if _, err := f.ReadAt(tail, tailStart); err != nil && err != io.EOF {
		return fmt.Errorf("mariadb: verify: read artifact tail: %w", err)
	}
	if !bytes.Contains(tail, []byte(dumpTrailerMarker)) {
		return fmt.Errorf("mariadb: verify: artifact is missing mariadb-dump's %q trailer - dump looks truncated", dumpTrailerMarker)
	}
	return nil
}

func (p *Plugin) Health(ctx context.Context) error {
	if err := p.exec.Run(ctx, "echo ok", nil, io.Discard); err != nil {
		return fmt.Errorf("mariadb: health check: %w", err)
	}
	return nil
}

func (p *Plugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "mariadb", Version: "0.1.0"}
}
