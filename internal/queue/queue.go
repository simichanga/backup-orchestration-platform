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
type Queue interface {
	Enqueue(core.Job) error
	Dequeue(ctx context.Context) (core.Job, error)
	Len() int
}
