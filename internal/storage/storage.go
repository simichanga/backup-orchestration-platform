// Package storage defines the port over a backup repository. The controller
// only ever talks to this interface, never to a specific backend directly.
package storage

import "bop/internal/core"

// StorageProvider abstracts a backup repository. The initial implementation
// wraps Restic; future implementations could wrap Borg, MinIO, or S3 without
// requiring any change to plugins or the controller.
type StorageProvider interface {
	// Store uploads an artifact and returns its snapshot ID.
	Store(core.Artifact) (core.SnapshotID, error)

	// Retrieve downloads a stored snapshot into the given artifact's path.
	Retrieve(core.SnapshotID, core.Artifact) error

	// Verify confirms a stored snapshot is intact (checksums, repo integrity).
	Verify(core.SnapshotID) error

	// Delete removes a stored snapshot.
	Delete(core.SnapshotID) error

	// Snapshots lists all snapshots in the repository.
	Snapshots() ([]core.Snapshot, error)

	// ApplyRetention prunes snapshots that fall outside the given policy.
	ApplyRetention(core.Policy) error
}
