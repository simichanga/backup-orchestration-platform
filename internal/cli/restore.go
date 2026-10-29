package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bop/internal/core"
)

// newRestoreCmd retrieves a snapshot's raw artifact to a filesystem target,
// matching the documented quickstart example
// (bop restore --snapshot abc123 --target /tmp/restored-db). This is
// StorageProvider.Retrieve, not a plugin-specific restore-into-a-live-
// database operation - the latter is a separate concern (see
// BackupPlugin.Restore's contract) that this command does not perform.
//
// With storage.repositories configured, snapshots live in more than one
// repository: the one to read from is the snapshot's inventory server's
// (--host, or looked up from metadata by snapshot ID), falling back to the
// default repository for snapshots metadata doesn't know.
func newRestoreCmd(configPath *string) *cobra.Command {
	var snapshotID, target, host string

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a snapshot's artifact to a target path",
		RunE: func(cmd *cobra.Command, args []string) error {
			if snapshotID == "" || target == "" {
				return fmt.Errorf("--snapshot and --target are required")
			}

			a, err := buildApp(*configPath)
			if err != nil {
				return err
			}
			defer a.Close()

			if host == "" {
				if host, err = snapshotHost(cmd.Context(), a, snapshotID); err != nil {
					return err
				}
			}
			sp := a.Controller.Storage
			if host != "" {
				if sp, err = a.Controller.StorageFor(host); err != nil {
					return fmt.Errorf("restore: %w", err)
				}
			}

			artifact := core.Artifact{Path: target}
			if err := sp.Retrieve(cmd.Context(), core.SnapshotID(snapshotID), artifact); err != nil {
				return fmt.Errorf("restore: %w", err)
			}

			a.Logger.Info("restore completed", "snapshot", snapshotID, "target", target)
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "snapshot ID to restore")
	cmd.Flags().StringVar(&target, "target", "", "target path to restore into")
	cmd.Flags().StringVar(&host, "host", "", "inventory host the snapshot belongs to (default: looked up from metadata)")
	return cmd
}

// snapshotHost finds which inventory server recorded snapshotID (a full ID
// or a unique prefix, as restic accepts), or "" if metadata has no record.
func snapshotHost(ctx context.Context, a *app, snapshotID string) (string, error) {
	var found []string
	for host := range a.Controller.Inventory.Servers {
		snaps, err := a.Controller.Metadata.ListSnapshots(ctx, host)
		if err != nil {
			return "", fmt.Errorf("restore: %w", err)
		}
		for _, s := range snaps {
			if strings.HasPrefix(string(s.ID), snapshotID) {
				found = append(found, host)
				break
			}
		}
	}
	if len(found) > 1 {
		return "", fmt.Errorf("restore: snapshot %q matches snapshots of several hosts (%s) - use a longer ID or --host", snapshotID, strings.Join(found, ", "))
	}
	if len(found) == 0 {
		return "", nil
	}
	return found[0], nil
}
