// Package queue defines the port between the scheduler and the controller.
package queue

import (
	"context"

	"bop/internal/core"
)

// Queue is the port the scheduler writes jobs to and the controller reads
// jobs from. The Phase 1 implementation is in-memory (see memory.go); later
// phases can swap in a shared queue (NATS, Redis) for multi-controller
// deployments without changing this interface or its callers.
//
// System of record: the metadata service, not the Queue, owns job
// durability. Callers must persist a Job as core.JobStatusQueued in the
// metadata service before calling Enqueue. An Enqueue failure (e.g. a full
// in-memory buffer) is then recoverable rather than a silently missed
// backup: the job row still exists in the "queued" state, and a reconciler
// can re-dispatch it. The Queue itself may drop work it cannot hold.
type Queue interface {
	Enqueue(core.Job) error
	Dequeue(ctx context.Context) (core.Job, error)
	Len() int
}
