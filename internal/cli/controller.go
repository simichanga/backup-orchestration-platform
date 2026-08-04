package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// newControllerCmd runs the controller daemon: loads inventory, registers
// plugins, and blocks. There is no scheduler wired in yet (a later step in
// the Phase 1 build order), so nothing dispatches jobs on its own -
// "bop backup" triggers a job manually in the meantime. Running this
// command is still real, useful infrastructure: it proves config and
// inventory load, opens the metadata store, cleans up orphaned jobs from a
// previous run, and shuts down cleanly on SIGINT/SIGTERM.
func newControllerCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "controller",
		Short: "Run the BOP controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildApp(*configPath)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			n, err := a.Metadata.FailOrphanedJobs(ctx)
			if err != nil {
				return fmt.Errorf("fail orphaned jobs: %w", err)
			}
			if n > 0 {
				a.Logger.Warn("marked orphaned jobs as failed on startup", "count", n)
			}

			a.Logger.Info("bop controller ready",
				"inventory_hosts", len(a.Inventory.Servers),
				"note", "scheduler not implemented yet; trigger jobs with 'bop backup'")

			<-ctx.Done()
			a.Logger.Info("shutting down")
			return nil
		},
	}
}
