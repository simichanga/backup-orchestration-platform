// Package cli implements the bop command-line interface documented in
// docs/03-getting-started/*.md. Phase 1 has no daemon/API for commands to
// talk to, so each command (other than "controller") builds its own
// in-process app and runs to completion.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"bop/internal/config"
	"bop/internal/controller"
	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/metrics"
	"bop/internal/plugin/filesystem"
	"bop/internal/plugin/postgres"
	"bop/internal/queue"
	"bop/internal/storage"
)

// queueCapacity is the in-memory Queue's buffer size. Fixed for Phase 1
// rather than configurable: a full queue is not a lost job (see
// internal/queue's durability contract and "bop controller"'s startup
// reconciliation), so sizing this precisely is not load-bearing yet.
const queueCapacity = 256

// app bundles everything a command needs. Callers must call Close when done.
type app struct {
	Config          *config.Config
	Inventory       *inventory.Inventory
	Metadata        *metadata.Store
	Queue           queue.Queue
	Controller      *controller.Controller
	MetricsRegistry *prometheus.Registry
	Logger          *slog.Logger
}

func (a *app) Close() error {
	return a.Metadata.Close()
}

// buildApp loads config.yaml and inventory.yaml, opens the metadata store,
// constructs the storage provider and controller, and registers all
// available plugins. Only restic (storage) is unconditionally supported -
// metadata now has two real drivers (sqlite, postgres; postgres is the
// documented prerequisite for multi-controller HA, see
// docs/06-high-availability.md) - buildApp fails clearly on any other
// configured value rather than pretending to support it.
func buildApp(configPath string) (*app, error) {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Logging)
	slog.SetDefault(logger)

	inv, err := inventory.Load(cfg.Inventory)
	if err != nil {
		return nil, fmt.Errorf("load inventory: %w", err)
	}

	var md *metadata.Store
	switch cfg.Metadata.Driver {
	case "sqlite":
		md, err = metadata.Open(cfg.Metadata.DSN)
	case "postgres":
		md, err = metadata.OpenPostgres(cfg.Metadata.DSN)
	default:
		return nil, fmt.Errorf("metadata.driver %q is not yet implemented (only sqlite, postgres)", cfg.Metadata.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open metadata store: %w", err)
	}

	if cfg.Storage.Provider != "restic" {
		md.Close()
		return nil, fmt.Errorf("storage.provider %q is not yet implemented (only restic)", cfg.Storage.Provider)
	}
	// storage.restic.concurrency is documented but not yet enforced by
	// ResticProvider - noted here rather than silently ignored.
	sp := storage.NewResticProvider("restic", cfg.Storage.Restic.Repository, cfg.Storage.Restic.PasswordFile, cfg.Storage.Restic.PasswordEnv, cfg.Storage.Restic.ExtraArgs)

	ctl := controller.New(inv, md, sp, cfg.Verification, cfg.Controller.TempDir, cfg.Controller.JobTimeout)
	ctl.Logger = logger

	// Metrics subscribe to the same event stream as logging, so recording
	// them requires zero extra instrumentation calls inside the controller
	// or plugins - the "Metrics/Events" step of the documented core backup
	// pipeline (present at every phase, per docs/resources/scalability-model.png).
	reg := prometheus.NewRegistry()
	metricsPub := metrics.New(reg)
	ctl.Events = &events.Multi{
		Subscribers: []events.Publisher{
			&events.LogPublisher{Logger: logger},
			metricsPub,
			&metadata.EventPublisher{Store: md, Logger: logger},
		},
		Logger: logger,
	}

	ctl.RegisterPlugin("postgres", postgres.NewFactory(cfg.SSH.KnownHostsFile))
	ctl.RegisterPlugin("filesystem", filesystem.NewFactory(cfg.SSH.KnownHostsFile))

	q := queue.NewMemory(queueCapacity)

	return &app{Config: cfg, Inventory: inv, Metadata: md, Queue: q, Controller: ctl, MetricsRegistry: reg, Logger: logger}, nil
}

// reconcileQueuedJobs re-enqueues every job persisted as queued. The
// in-memory Queue never survives a process restart, but the metadata
// service does (see internal/queue's durability contract) - this is what
// actually rehydrates the queue from that persisted state on every
// "bop controller" startup, rather than leaving the contract's recovery
// promise unfulfilled.
func (a *app) reconcileQueuedJobs(ctx context.Context) error {
	jobs, err := a.Metadata.ListJobsByStatus(ctx, core.JobStatusQueued)
	if err != nil {
		return fmt.Errorf("list queued jobs: %w", err)
	}
	for _, job := range jobs {
		if err := a.Queue.Enqueue(job); err != nil {
			a.Logger.Error("reconcile: enqueue failed, job remains queued", "job_id", job.ID, "error", err)
		}
	}
	if len(jobs) > 0 {
		a.Logger.Info("reconciled queued jobs from a previous run", "count", len(jobs))
	}
	return nil
}

// runConsumer drains the queue and runs jobs one at a time, serially, until
// ctx is cancelled. Deliberately not a worker pool for Phase 1: the
// documented scalability model is single-controller, and ApplyRetention's
// restic "forget --prune" takes an exclusive repository lock - concurrent
// jobs against the same repository would collide on it. A single consumer
// makes that structurally impossible instead of requiring a
// prune-serialization mechanism that doesn't exist yet.
// controller.concurrency is documented but deliberately unenforced until a
// future phase's worker pool needs it.
func (a *app) runConsumer(ctx context.Context) {
	for {
		job, err := a.Queue.Dequeue(ctx)
		if err != nil {
			return // ctx cancelled: normal shutdown
		}
		if err := a.Controller.RunJob(ctx, job); err != nil {
			a.Logger.Error("job failed", "job_id", job.ID, "host", job.Host, "plugin", job.Plugin, "error", err)
		}
	}
}

// eventPruneInterval governs how often runEventPruner re-checks, not how
// long events are kept (that's metadata.event_retention) - not
// configurable for v1, since a fixed hourly cadence is cheap (an indexed
// DELETE that usually finds nothing to do) and there's no operational
// reason yet to tune it independently of the retention window itself.
const eventPruneInterval = time.Hour

// runEventPruner deletes events older than cfg.Metadata.EventRetention,
// once immediately (so a controller that was down doesn't wait a full
// interval to catch up) and then every eventPruneInterval until ctx is
// cancelled. Without this, the events table - written to many times per
// job, unlike jobs/snapshots - would grow unbounded across a long-running
// controller's uptime.
func (a *app) runEventPruner(ctx context.Context) {
	prune := func() {
		// A fresh, short-lived context, deliberately not ctx: a prune that
		// has already started should be allowed to finish even if the
		// controller begins shutting down (ctx cancelled) at the same
		// moment - the same reasoning as controller.RunJob's recordCtx.
		pruneCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cutoff := time.Now().UTC().Add(-a.Config.Metadata.EventRetention)
		n, err := a.Metadata.PruneEventsOlderThan(pruneCtx, cutoff)
		if err != nil {
			a.Logger.Error("event pruning failed", "error", err)
			return
		}
		if n > 0 {
			a.Logger.Info("pruned old events", "count", n, "cutoff", cutoff)
		}
	}

	prune()

	ticker := time.NewTicker(eventPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}

func newLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
