// Package filesystem implements the BackupPlugin port for arbitrary
// directories, documented as one of BOP's supported data sources. Connects
// to the target host over SSH (srv.ssh_user/ssh_key from inventory.yaml)
// and streams a tar+gzip archive of each configured path.
package filesystem

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

// gzipMagic is the two-byte header every gzip stream starts with - the
// cheap structural check Verify uses to confirm Backup produced a real
// archive, not a truncated or empty stream.
var gzipMagic = []byte{0x1f, 0x8b}

type Plugin struct {
	exec    sshexec.Executor
	cfg     filesystemConfig
	tempDir string
}

// NewFactory returns a controller.PluginFactory-compatible factory that
// builds a filesystem Plugin from a server's inventory entry and this
// plugin's config block.
func NewFactory() func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
		fsCfg, err := parseConfig(cfg)
		if err != nil {
			return nil, err
		}
		if srv.Host == "" {
			return nil, fmt.Errorf("filesystem: server host is required")
		}
		if srv.SSHUser == "" || srv.SSHKey == "" {
			return nil, fmt.Errorf("filesystem: ssh_user and ssh_key are required")
		}

		return &Plugin{
			exec:    &sshexec.SSHExecutor{Addr: srv.Host + ":22", User: srv.SSHUser, KeyPath: srv.SSHKey},
			cfg:     fsCfg,
			tempDir: tempDir,
		}, nil
	}
}

func (p *Plugin) Discover(context.Context) ([]core.Resource, error) {
	resources := make([]core.Resource, len(p.cfg.Paths))
	for i, path := range p.cfg.Paths {
		resources[i] = core.Resource{ID: path, Name: path}
	}
	return resources, nil
}

func (p *Plugin) Backup(ctx context.Context, res core.Resource) (core.Artifact, error) {
	f, err := os.CreateTemp(p.tempDir, "fs-*.tar.gz")
	if err != nil {
		return core.Artifact{}, fmt.Errorf("filesystem: create artifact file: %w", err)
	}
	path := f.Name()

	// Close before Remove on every error path, not after via defer: an
	// open file handle can't be removed on Windows (unlike POSIX, which
	// allows unlinking an open file), and closing before removing is
	// correct on both regardless.
	if err := p.exec.Run(ctx, tarCommand(p.cfg, res.ID), nil, f); err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("filesystem: tar %s: %w", res.ID, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("filesystem: stat artifact: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("filesystem: close artifact: %w", err)
	}

	return core.Artifact{
		ResourceID: res.ID,
		Path:       path,
		Size:       info.Size(),
		CreatedAt:  time.Now(),
	}, nil
}

// Restore extracts a.Path's tar.gz archive into a.ResourceID, a directory
// it creates if missing. Also the restore-test pipeline step (see
// plugin.BackupPlugin.Restore's doc comment): during verification,
// a.ResourceID is a scratch-suffixed path, never the live resource, so
// this genuinely proves recoverability into a real, disposable directory.
// Unlike the postgres plugin, filesystem needs no separate
// scratch-provisioning story - "create a new directory" is always safe,
// which is why verification.enabled is actually functional for this
// plugin even though it currently is not for postgres.
func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("filesystem: open artifact: %w", err)
	}
	defer f.Close()

	if err := p.exec.Run(ctx, untarCommand(a.ResourceID), f, io.Discard); err != nil {
		return fmt.Errorf("filesystem: restore %s: %w", a.ResourceID, err)
	}
	return nil
}

// Verify is a cheap structural check, not a full tar/gzip parse: non-empty,
// and starts with gzip's magic bytes. This is the "is the artifact
// well-formed" tier of the three-tier verification model (see
// docs/01-introduction.md), meant to catch a truncated or empty archive
// before the controller spends time checksumming and uploading it.
func (p *Plugin) Verify(ctx context.Context, a core.Artifact) error {
	info, err := os.Stat(a.Path)
	if err != nil {
		return fmt.Errorf("filesystem: verify: stat artifact: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("filesystem: verify: artifact is empty")
	}

	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("filesystem: verify: open artifact: %w", err)
	}
	defer f.Close()

	magic := make([]byte, len(gzipMagic))
	if _, err := io.ReadFull(f, magic); err != nil {
		return fmt.Errorf("filesystem: verify: read artifact: %w", err)
	}
	if !bytes.Equal(magic, gzipMagic) {
		return fmt.Errorf("filesystem: verify: artifact does not look like a gzip stream (missing magic bytes)")
	}
	return nil
}

func (p *Plugin) Health(ctx context.Context) error {
	if err := p.exec.Run(ctx, "echo ok", nil, io.Discard); err != nil {
		return fmt.Errorf("filesystem: health check: %w", err)
	}
	return nil
}

func (p *Plugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "filesystem", Version: "0.1.0"}
}
