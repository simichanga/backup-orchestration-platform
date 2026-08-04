package queue

import (
	"context"
	"errors"

	"bop/internal/core"
)

// ErrQueueFull is returned by Enqueue when the memory queue is at capacity.
var ErrQueueFull = errors.New("queue: full")

// Memory is an in-memory Queue backed by a buffered channel. It is the
// Phase 1 default: single controller binary, no external dependencies.
type Memory struct {
	jobs chan core.Job
}

// NewMemory creates an in-memory Queue with the given capacity.
func NewMemory(capacity int) *Memory {
	return &Memory{jobs: make(chan core.Job, capacity)}
}

func (m *Memory) Enqueue(job core.Job) error {
	select {
	case m.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

func (m *Memory) Dequeue(ctx context.Context) (core.Job, error) {
	select {
	case job := <-m.jobs:
		return job, nil
	case <-ctx.Done():
		return core.Job{}, ctx.Err()
	}
}

func (m *Memory) Len() int {
	return len(m.jobs)
}
