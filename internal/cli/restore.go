package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bop/internal/core"
)

// newRestoreCmd retrieves a snapshot's raw artifact to a filesystem target,
// matching the documented quickstart example
// (bop restore --snapshot abc123 --target /tmp/restored-db). This is
// StorageProvider.Retrieve, not a plugin-specific restore-into-a-live-
// database operation - the latter is a separate concern (see
// BackupPlugin.Restore's contract) that this command does not perform.
func newRestoreCmd(configPath *string) *cobra.Command {
	var snapshotID, target string

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

			artifact := core.Artifact{Path: target}
			if err := a.Controller.Storage.Retrieve(cmd.Context(), core.SnapshotID(snapshotID), artifact); err != nil {
				return fmt.Errorf("restore: %w", err)
			}

			a.Logger.Info("restore completed", "snapshot", snapshotID, "target", target)
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "snapshot ID to restore")
	cmd.Flags().StringVar(&target, "target", "", "target path to restore into")
	return cmd
}
