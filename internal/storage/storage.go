// Package storage defines the port over a backup repository. The controller
// only ever talks to this interface, never to a specific backend directly.
package storage

import (
	"context"

	"bop/internal/core"
)

// StorageProvider abstracts a backup repository. The initial implementation
// wraps Restic; future implementations could wrap Borg, MinIO, or S3
// without requiring any change to plugins or the controller.
//
// Every method takes a context.Context: these do I/O (subprocess execution,
// network calls) that must be cancellable, notably by controller.job_timeout.
type StorageProvider interface {
	// Store uploads an artifact and returns its snapshot ID.
	Store(ctx context.Context, artifact core.Artifact) (core.SnapshotID, error)

	// Retrieve downloads a stored snapshot into the given artifact's path.
	Retrieve(ctx context.Context, id core.SnapshotID, artifact core.Artifact) error

	// Verify confirms a stored snapshot is intact (checksums, repo integrity).
	Verify(ctx context.Context, id core.SnapshotID) error

	// Delete removes a stored snapshot.
	Delete(ctx context.Context, id core.SnapshotID) error

	// Snapshots lists all snapshots in the repository.
	Snapshots(ctx context.Context) ([]core.Snapshot, error)

	// ApplyRetention prunes snapshots for host that fall outside policy.
	// host scopes the operation: verified against restic directly that an
	// unscoped forget applies its keep-policy repository-wide, across every
	// host and resource, not just the one the calling job is responsible
	// for - which would silently apply the wrong retention policy to other
	// hosts' backups.
	ApplyRetention(ctx context.Context, host string, policy core.Policy) error
}
