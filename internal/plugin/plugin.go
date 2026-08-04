// Package plugin defines the port every backup source implements. The
// controller treats all plugins identically through this interface.
package plugin

import (
	"context"

	"bop/internal/core"
)

// BackupPlugin is the contract every backup source (PostgreSQL, Docker,
// filesystem, ...) implements. The controller never contains source-specific
// logic; it only calls these methods.
//
// Every method except Metadata takes a context.Context: these do I/O (SSH
// connections, subprocess execution, database queries) that must be
// cancellable, notably by controller.job_timeout.
type BackupPlugin interface {
	// Discover lists the resources this plugin can back up on its target host.
	Discover(ctx context.Context) ([]core.Resource, error)

	// Backup produces an artifact from a single resource.
	Backup(ctx context.Context, resource core.Resource) (core.Artifact, error)

	// Restore writes an artifact back to its target. Also used, with a
	// temporary target, as the optional restore-test pipeline step.
	Restore(ctx context.Context, artifact core.Artifact) error

	// Verify is a structural sanity check on an artifact (e.g. is a dump
	// file parseable, is a tar stream not truncated). It runs immediately
	// after Backup, before checksum/encryption/upload, and is distinct from
	// StorageProvider.Verify (storage-level integrity) and a restore test
	// (actual recoverability).
	Verify(ctx context.Context, artifact core.Artifact) error

	// Health reports whether the plugin can currently reach its target.
	Health(ctx context.Context) error

	// Metadata identifies this plugin implementation and its version. It
	// does no I/O, so it takes no context.
	Metadata() core.PluginMetadata
}
