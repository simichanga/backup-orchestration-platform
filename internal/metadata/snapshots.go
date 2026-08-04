package metadata

import (
	"context"
	"fmt"

	"bop/internal/core"
)

// RecordSnapshot persists a stored artifact's metadata.
func (s *Store) RecordSnapshot(ctx context.Context, snap core.Snapshot) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO snapshots (id, host, plugin, size, checksum, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.Host, snap.Plugin, snap.Size, snap.Checksum, snap.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("metadata: record snapshot %s: %w", snap.ID, err)
	}
	return nil
}

// ListSnapshots returns all snapshots for a host, most recent first.
func (s *Store) ListSnapshots(ctx context.Context, host string) ([]core.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, host, plugin, size, checksum, created_at
		FROM snapshots WHERE host = ? ORDER BY created_at DESC`, host)
	if err != nil {
		return nil, fmt.Errorf("metadata: list snapshots for %s: %w", host, err)
	}
	defer rows.Close()

	var snaps []core.Snapshot
	for rows.Next() {
		var snap core.Snapshot
		if err := rows.Scan(&snap.ID, &snap.Host, &snap.Plugin, &snap.Size, &snap.Checksum, &snap.CreatedAt); err != nil {
			return nil, fmt.Errorf("metadata: scan snapshot: %w", err)
		}
		snaps = append(snaps, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: list snapshots for %s: %w", host, err)
	}
	return snaps, nil
}
