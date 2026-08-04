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
	"log/slog"
	"os"
	"strings"
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

// Restore restores a.Path's dump into a.ResourceID. When a.ResourceID
// carries plugin.RestoreTestSuffix (the controller's restore-test step),
// the target database doesn't exist yet - postgres has no "create if
// missing" restore mode - so Restore provisions a scratch database first
// and drops it afterward, best-effort. It refuses to touch a database that
// already existed before this call: CREATE DATABASE failing with "already
// exists" means either a real (non-scratch) resource collides with this
// name, or a previous restore-test crashed before cleaning up - either way,
// this run didn't create it and must not restore into or drop it.
func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("postgres: open artifact: %w", err)
	}
	defer f.Close()

	isRestoreTest := strings.HasSuffix(a.ResourceID, plugin.RestoreTestSuffix)
	if isRestoreTest {
		if err := p.exec.Run(ctx, createDatabaseCommand(p.cfg, a.ResourceID), nil, io.Discard); err != nil {
			if isAlreadyExistsError(err) {
				return fmt.Errorf("postgres: restore-test scratch database %q already exists - not created by this run, refusing to restore into or drop it; a previous restore-test may have crashed before cleanup, drop it manually to retry: %w", a.ResourceID, err)
			}
			return fmt.Errorf("postgres: create restore-test scratch database %q: %w", a.ResourceID, err)
		}
	}

	if err := p.exec.Run(ctx, restoreCommand(p.cfg, a.ResourceID), f, io.Discard); err != nil {
		if isRestoreTest {
			p.dropScratchDatabase(ctx, a.ResourceID)
		}
		return fmt.Errorf("postgres: restore %s: %w", a.ResourceID, err)
	}

	if isRestoreTest {
		p.dropScratchDatabase(ctx, a.ResourceID)
	}
	return nil
}

// dropScratchDatabase tears down a restore-test's scratch database,
// best-effort: a failed teardown must not fail an otherwise-successful
// restore-test, mirroring the filesystem plugin's identical treatment of
// its own restore-test scratch directory cleanup. Left-behind databases are
// still safe - the next restore-test for the same resource will refuse to
// reuse or drop it (see Restore's isAlreadyExistsError handling) rather
// than silently operating on state this run didn't create.
func (p *Plugin) dropScratchDatabase(ctx context.Context, database string) {
	if err := p.exec.Run(ctx, dropDatabaseCommand(p.cfg, database), nil, io.Discard); err != nil {
		slog.Default().Warn("postgres: restore-test scratch database cleanup failed", "database", database, "error", err)
	}
}

// isAlreadyExistsError reports whether err is CREATE DATABASE's specific
// "already exists" failure, distinct from other failures (e.g. missing
// CREATEDB privilege) that should be reported as-is rather than
// reinterpreted as an ownership conflict. Matched by substring since the
// executor abstraction carries the remote command's stderr as plain text,
// not a structured Postgres error code - consistent with Verify's
// pg_dump-header substring check elsewhere in this file.
func isAlreadyExistsError(err error) bool {
	return strings.Contains(err.Error(), "already exists")
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
