// Package scheduler creates jobs from inventory.yaml's per-server schedule
// (a standard 5-field cron expression) and hands them to the controller via
// the Queue port, as documented in docs/02-architecture.md's Scheduler
// section. It runs in-process within the controller binary for Phase 1.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/queue"
)

// Scheduler fires a job for every plugin on every server that has a
// schedule configured, at the times its cron expression names. Servers with
// no schedule are skipped entirely - they stay manual-only, triggered via
// "bop backup".
type Scheduler struct {
	inventory *inventory.Inventory
	metadata  *metadata.Store
	queue     queue.Queue
	events    events.Publisher
	logger    *slog.Logger

	cron *cron.Cron
}

// New builds a Scheduler and validates every server's schedule expression
// up front - inventory.Validate() explicitly defers cron syntax checking
// here, since this package is the one that owns the cron library. A bad
// expression is a startup error, not a silently-never-firing schedule.
//
// cronLocation is config.yaml's scheduler.cron_location (e.g. "Local",
// "UTC", "America/New_York"); empty or "Local" uses the machine's local
// time zone.
func New(inv *inventory.Inventory, md *metadata.Store, q queue.Queue, pub events.Publisher, cronLocation string, logger *slog.Logger) (*Scheduler, error) {
	loc := time.Local
	if cronLocation != "" && cronLocation != "Local" {
		var err error
		loc, err = time.LoadLocation(cronLocation)
		if err != nil {
			return nil, fmt.Errorf("scheduler: invalid scheduler.cron_location %q: %w", cronLocation, err)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if pub == nil {
		pub = &events.LogPublisher{Logger: logger}
	}

	s := &Scheduler{
		inventory: inv,
		metadata:  md,
		queue:     q,
		events:    pub,
		logger:    logger,
		cron:      cron.New(cron.WithLocation(loc)),
	}

	for host, srv := range inv.Servers {
		if srv.Schedule == "" {
			continue
		}
		host, srv := host, srv // capture for the closure below
		if _, err := s.cron.AddFunc(srv.Schedule, func() { s.dispatch(host, srv) }); err != nil {
			return nil, fmt.Errorf("scheduler: server %q: invalid schedule %q: %w", host, srv.Schedule, err)
		}
	}

	return s, nil
}

// Start begins firing schedules in the background. Non-blocking.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop halts the scheduler. It waits for any in-progress dispatch (job
// creation, not the backup itself - dispatch only creates and enqueues)
// to finish, then returns.
func (s *Scheduler) Stop() {
	<-s.cron.Stop().Done()
}

// publish emits an event, recovering from a panicking Publisher. dispatch
// runs inside robfig/cron's own goroutine, which does not recover panics
// by default - an unrecovered panic here would crash the whole process,
// not just fail one dispatch. Mirrors controller.publish for the same
// reason: event emission must never be able to take down a backup path.
func (s *Scheduler) publish(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("event publisher panicked", "panic", r, "event_type", e.Type)
		}
	}()
	e.Timestamp = time.Now().UTC()
	s.events.Publish(ctx, e)
}

// dispatch creates and enqueues one job per plugin configured on srv. A
// server-level schedule tick must not collapse to a single job: a host
// running both postgres and filesystem plugins needs both backed up on
// that tick, not just one.
func (s *Scheduler) dispatch(host string, srv inventory.Server) {
	ctx := context.Background()
	for pluginName := range srv.Plugins {
		job := core.NewJob(host, pluginName, srv.Retention)

		// Persist as queued before enqueueing, per the job-durability
		// contract (see internal/queue's Queue doc): the metadata service
		// is the system of record, not the in-memory Queue.
		if err := s.metadata.CreateJob(ctx, job); err != nil {
			s.logger.Error("scheduler: create job failed", "host", host, "plugin", pluginName, "error", err)
			continue
		}
		s.publish(ctx, events.Event{Type: events.TypeBackupRequested, JobID: job.ID, Host: host, Plugin: pluginName})

		if err := s.queue.Enqueue(job); err != nil {
			// The job row is already persisted as queued, so it is not
			// lost - "bop controller"'s startup reconciliation re-enqueues
			// any job still in the queued state, including this one.
			s.logger.Error("scheduler: enqueue failed, job left queued for reconciliation", "job_id", job.ID, "host", host, "plugin", pluginName, "error", err)
		}
	}
}
