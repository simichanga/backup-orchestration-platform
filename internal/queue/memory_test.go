package queue

import (
	"context"
	"testing"
	"time"

	"bop/internal/core"
)

func TestMemoryEnqueueDequeue(t *testing.T) {
	q := NewMemory(1)
	job := core.Job{ID: "job-1"}

	if err := q.Enqueue(job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := q.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got.ID != job.ID {
		t.Fatalf("Dequeue = %+v, want %+v", got, job)
	}
}

func TestMemoryEnqueueFullReturnsErrQueueFull(t *testing.T) {
	q := NewMemory(1)
	if err := q.Enqueue(core.Job{ID: "job-1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// The buffer is now full. Per the Queue contract, this must return an
	// error rather than block or silently drop, so a caller can fall back
	// to the durable job row instead of losing the backup.
	err := q.Enqueue(core.Job{ID: "job-2"})
	if err != ErrQueueFull {
		t.Fatalf("Enqueue on full queue = %v, want ErrQueueFull", err)
	}
}

func TestMemoryDequeueRespectsContextCancellation(t *testing.T) {
	q := NewMemory(1)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := q.Dequeue(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("Dequeue on empty queue = %v, want context.DeadlineExceeded", err)
	}
}
