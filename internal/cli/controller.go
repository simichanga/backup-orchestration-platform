package cli

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	"bop/internal/scheduler"
)

// newControllerCmd runs the controller daemon: loads inventory, registers
// plugins, starts the scheduler, and runs a single serial consumer that
// drains the queue. Shuts down cleanly on SIGINT/SIGTERM - a job in
// progress at shutdown ends up failed, not stuck in_progress (see
// controller.RunJob's recordCtx handling), consistent with the crash
// recovery model rather than needing a separate code path for it.
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

			if err := a.reconcileQueuedJobs(ctx); err != nil {
				return fmt.Errorf("reconcile queued jobs: %w", err)
			}

			sched, err := scheduler.New(a.Inventory, a.Metadata, a.Queue, a.Controller.Events, a.Config.Scheduler.CronLocation, a.Logger)
			if err != nil {
				return fmt.Errorf("build scheduler: %w", err)
			}
			sched.Start()
			defer sched.Stop()

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.runConsumer(ctx)
			}()

			a.Logger.Info("bop controller ready", "inventory_hosts", len(a.Inventory.Servers))

			<-ctx.Done()
			a.Logger.Info("shutting down")
			wg.Wait()
			return nil
		},
	}
}
