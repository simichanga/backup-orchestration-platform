package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bop/internal/core"
)

// newBackupCmd triggers a backup immediately. Phase 1 has no daemon/API
// for this to talk to, so it builds a full app and runs the job in-process
// rather than dispatching to a running "bop controller".
func newBackupCmd(configPath *string) *cobra.Command {
	var host, pluginName string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Trigger an immediate backup for a host and plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || pluginName == "" {
				return fmt.Errorf("--host and --plugin are required")
			}

			a, err := buildApp(*configPath)
			if err != nil {
				return err
			}
			defer a.Close()

			srv, ok := a.Inventory.Servers[host]
			if !ok {
				return fmt.Errorf("host %q not found in inventory", host)
			}

			ctx := cmd.Context()
			job := core.NewJob(host, pluginName, srv.Retention)

			// Persisted before running, per the job-durability contract
			// (see internal/queue's Queue doc): the metadata service is
			// the system of record for a job, not any in-memory state.
			if err := a.Metadata.CreateJob(ctx, job); err != nil {
				return fmt.Errorf("create job: %w", err)
			}
			a.Logger.Info("backup job queued", "job_id", job.ID, "host", host, "plugin", pluginName)

			if err := a.Controller.RunJob(ctx, job); err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			a.Logger.Info("backup completed", "job_id", job.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "inventory host to back up")
	cmd.Flags().StringVar(&pluginName, "plugin", "", "plugin to run")
	return cmd
}
