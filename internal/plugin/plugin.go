// Package plugin defines the port every backup source implements. The
// controller treats all plugins identically through this interface.
package plugin

import "bop/internal/core"

// BackupPlugin is the contract every backup source (PostgreSQL, Docker,
// filesystem, ...) implements. The controller never contains source-specific
// logic; it only calls these methods.
type BackupPlugin interface {
	// Discover lists the resources this plugin can back up on its target host.
	Discover() ([]core.Resource, error)

	// Backup produces an artifact from a single resource.
	Backup(core.Resource) (core.Artifact, error)

	// Restore writes an artifact back to its target. Also used, with a
	// temporary target, as the optional restore-test pipeline step.
	Restore(core.Artifact) error

	// Verify is a structural sanity check on an artifact (e.g. is a dump
	// file parseable, is a tar stream not truncated). It runs immediately
	// after Backup, before checksum/encryption/upload, and is distinct from
	// StorageProvider.Verify (storage-level integrity) and a restore test
	// (actual recoverability).
	Verify(core.Artifact) error

	// Health reports whether the plugin can currently reach its target.
	Health() error

	// Metadata identifies this plugin implementation and its version.
	Metadata() core.PluginMetadata
}
