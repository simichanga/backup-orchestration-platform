package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"bop/internal/api"
	"bop/internal/metrics"
	"bop/internal/scheduler"
)

// newControllerCmd runs the controller daemon: loads inventory, registers
// plugins, starts the metrics HTTP server, the optional read-only API
// server (api.enabled), and the scheduler, and runs a single serial
// consumer that drains the queue. Shuts down cleanly on
// SIGINT/SIGTERM - a job in progress at shutdown ends up failed, not stuck
// in_progress (see controller.RunJob's recordCtx handling), consistent
// with the crash recovery model rather than needing a separate code path
// for it.
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

			metricsAddr := fmt.Sprintf(":%d", a.Config.Metrics.Port)
			metricsServer, err := metrics.NewServer(metricsAddr, a.Config.Metrics.Path, a.MetricsRegistry)
			if err != nil {
				return fmt.Errorf("start metrics server: %w", err)
			}
			metricsErrCh := metricsServer.Start()
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := metricsServer.Shutdown(shutdownCtx); err != nil {
					a.Logger.Warn("metrics server shutdown", "error", err)
				}
			}()

			// apiErrCh stays nil (and so never fires) unless api.enabled -
			// a nil channel blocks forever in a select, which is exactly
			// "this case doesn't exist" for an optional server.
			var apiErrCh <-chan error
			if a.Config.API.Enabled {
				tokens, err := api.LoadTokens(a.Config.API)
				if err != nil {
					return fmt.Errorf("load api tokens: %w", err)
				}
				apiServer, err := api.NewServer(a.Config.API.Addr, tokens, a.Inventory, a.Metadata)
				if err != nil {
					return fmt.Errorf("start api server: %w", err)
				}
				apiErrCh = apiServer.Start()
				defer func() {
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := apiServer.Shutdown(shutdownCtx); err != nil {
						a.Logger.Warn("api server shutdown", "error", err)
					}
				}()
				a.Logger.Info("bop api server ready", "api_addr", apiServer.Addr())
			}

			sched, err := scheduler.New(a.Inventory, a.Metadata, a.Queue, a.Controller.Events, a.Config.Scheduler.CronLocation, a.Logger)
			if err != nil {
				return fmt.Errorf("build scheduler: %w", err)
			}
			sched.Start()
			defer sched.Stop()

			// runCtx, not ctx directly: the consumer must also stop if the
			// metrics server dies unexpectedly (metricsErrCh fires), not
			// only on a signal. Using ctx directly here would deadlock
			// wg.Wait() below in that case, since the consumer only reacts
			// to context cancellation, and ctx itself would still be live.
			runCtx, cancelRun := context.WithCancel(ctx)
			defer cancelRun()

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.runConsumer(runCtx)
			}()

			a.Logger.Info("bop controller ready",
				"inventory_hosts", len(a.Inventory.Servers),
				"metrics_addr", metricsServer.Addr(),
				"metrics_path", a.Config.Metrics.Path)

			select {
			case <-ctx.Done():
				a.Logger.Info("shutting down")
			case err := <-metricsErrCh:
				a.Logger.Error("metrics server exited unexpectedly", "error", err)
			case err := <-apiErrCh:
				a.Logger.Error("api server exited unexpectedly", "error", err)
			}
			cancelRun()
			wg.Wait()
			return nil
		},
	}
}
