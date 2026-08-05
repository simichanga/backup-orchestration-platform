package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"bop/internal/controller"
	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/queue"
)

type triggerBackupRequest struct {
	Host   string `json:"host"`
	Plugin string `json:"plugin"`
}

// triggerBackupHandler serves POST /v1/backups: {"host": "...", "plugin":
// "..."}. Mirrors internal/scheduler's dispatch, not "bop backup" - it
// persists the job as queued and enqueues it for the controller's own
// consumer goroutine to run, rather than running it inline. Running it
// inline (as the one-shot "bop backup" CLI command does) would let an API
// request execute concurrently with whatever runConsumer is doing at that
// moment, breaking the single-serial-consumer invariant the whole
// controller design exists to guarantee (see docs/05-operations.md's
// "One job runs at a time" note) - that invariant matters more inside a
// long-running "bop controller" process than it does for a one-shot CLI
// command with no consumer goroutine to collide with.
//
// ctl.BuildPlugin validates host/plugin the same way a real job run would
// (host in inventory, a factory registered for the plugin name) without
// actually instantiating anything long-lived - reusing it here means this
// endpoint rejects an invalid host/plugin combination immediately (404)
// instead of accepting it and only failing later inside the consumer,
// where the caller has no way to find out except by polling GET
// /v1/jobs/{id}.
func triggerBackupHandler(ctl *controller.Controller, q queue.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req triggerBackupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Host == "" || req.Plugin == "" {
			writeError(w, http.StatusBadRequest, "host and plugin are required")
			return
		}

		if _, err := ctl.BuildPlugin(req.Host, req.Plugin); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}

		ctx := r.Context()
		srv := ctl.Inventory.Servers[req.Host]
		job := core.NewJob(req.Host, req.Plugin, srv.Retention)

		// Persisted as queued before enqueueing, per the job-durability
		// contract (see internal/queue's Queue doc): the metadata service
		// is the system of record, not the in-memory Queue.
		if err := ctl.Metadata.CreateJob(ctx, job); err != nil {
			writeError(w, http.StatusInternalServerError, "create job: "+err.Error())
			return
		}
		if ctl.Events != nil {
			ctl.Events.Publish(ctx, events.Event{
				Type: events.TypeBackupRequested, JobID: job.ID, Host: req.Host, Plugin: req.Plugin,
				Timestamp: time.Now().UTC(),
			})
		}

		if err := q.Enqueue(job); err != nil {
			// The job row is already persisted as queued, so it is not
			// lost - "bop controller"'s startup reconciliation re-enqueues
			// any job still in the queued state, including this one, same
			// as the scheduler's own dispatch handles this.
			slog.Default().Error("api: enqueue failed, job left queued for reconciliation", "job_id", job.ID, "host", req.Host, "plugin", req.Plugin, "error", err)
		}

		writeJSON(w, http.StatusAccepted, newJobSummary(job))
	}
}
