package metadata

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"bop/internal/core"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	job := core.Job{
		ID:       "job-1",
		Host:     "prod-db",
		Plugin:   "postgres",
		Status:   core.JobStatusQueued,
		Policy:   core.Policy{Daily: 7, Weekly: 4, Monthly: 3},
		QueuedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	got, err := s.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Host != job.Host || got.Plugin != job.Plugin || got.Status != job.Status {
		t.Errorf("GetJob = %+v, want %+v", got, job)
	}
	if got.Policy != job.Policy {
		t.Errorf("GetJob.Policy = %+v, want %+v", got.Policy, job.Policy)
	}
	// SQLite has no native datetime type; modernc round-trips time.Time
	// through a string layout, which can silently shift precision or
	// location. Use Equal, not ==, since == also compares monotonic
	// reading and location, not just the instant.
	if !got.QueuedAt.Equal(job.QueuedAt) {
		t.Errorf("GetJob.QueuedAt = %v, want %v", got.QueuedAt, job.QueuedAt)
	}
}

func TestGetJobNotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetJob(context.Background(), "missing")
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("GetJob = %v, want ErrJobNotFound", err)
	}
}

func TestUpdateJobStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := s.UpdateJobStatus(ctx, "job-1", core.JobStatusInProgress); err != nil {
		t.Fatalf("UpdateJobStatus: %v", err)
	}

	got, err := s.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusInProgress {
		t.Errorf("Status = %q, want in_progress", got.Status)
	}
}

func TestUpdateJobStatusNotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.UpdateJobStatus(context.Background(), "missing", core.JobStatusFailed)
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("UpdateJobStatus = %v, want ErrJobNotFound", err)
	}
}

func TestFailOrphanedJobs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	jobs := []core.Job{
		{ID: "in-progress-1", Status: core.JobStatusInProgress, QueuedAt: time.Now().UTC()},
		{ID: "in-progress-2", Status: core.JobStatusInProgress, QueuedAt: time.Now().UTC()},
		{ID: "queued-1", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()},
		{ID: "completed-1", Status: core.JobStatusCompleted, QueuedAt: time.Now().UTC()},
	}
	for _, j := range jobs {
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(%s): %v", j.ID, err)
		}
	}

	n, err := s.FailOrphanedJobs(ctx)
	if err != nil {
		t.Fatalf("FailOrphanedJobs: %v", err)
	}
	if n != 2 {
		t.Fatalf("FailOrphanedJobs returned %d, want 2", n)
	}

	wantStatus := map[string]core.JobStatus{
		"in-progress-1": core.JobStatusFailed,
		"in-progress-2": core.JobStatusFailed,
		"queued-1":      core.JobStatusQueued,
		"completed-1":   core.JobStatusCompleted,
	}
	for id, want := range wantStatus {
		got, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob(%s): %v", id, err)
		}
		if got.Status != want {
			t.Errorf("job %s status = %q, want %q", id, got.Status, want)
		}
	}
}

func TestListJobsByStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	jobs := []core.Job{
		{ID: "queued-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: now},
		{ID: "queued-2", Host: "prod-db", Plugin: "filesystem", Status: core.JobStatusQueued, QueuedAt: now.Add(time.Minute)},
		{ID: "in-progress-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusInProgress, QueuedAt: now},
	}
	for _, j := range jobs {
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(%s): %v", j.ID, err)
		}
	}

	got, err := s.ListJobsByStatus(ctx, core.JobStatusQueued)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListJobsByStatus returned %d jobs, want 2", len(got))
	}
	if got[0].ID != "queued-1" || got[1].ID != "queued-2" {
		t.Errorf("ListJobsByStatus order = [%s, %s], want [queued-1, queued-2]", got[0].ID, got[1].ID)
	}
}

func TestListJobsByStatusEmpty(t *testing.T) {
	s := openTestStore(t)
	got, err := s.ListJobsByStatus(context.Background(), core.JobStatusQueued)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListJobsByStatus returned %d jobs, want 0", len(got))
	}
}

func TestRecordAndListSnapshots(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	snaps := []core.Snapshot{
		{ID: "snap-1", JobID: "job-1", Host: "prod-db", Plugin: "postgres", Size: 100, Checksum: "sha256:aaa", CreatedAt: now},
		{ID: "snap-2", JobID: "job-2", Host: "prod-db", Plugin: "postgres", Size: 200, Checksum: "sha256:bbb", CreatedAt: now.Add(time.Minute)},
		{ID: "snap-3", JobID: "job-3", Host: "other-host", Plugin: "postgres", Size: 300, Checksum: "sha256:ccc", CreatedAt: now},
	}
	for _, snap := range snaps {
		if err := s.RecordSnapshot(ctx, snap); err != nil {
			t.Fatalf("RecordSnapshot(%s): %v", snap.ID, err)
		}
	}

	got, err := s.ListSnapshots(ctx, "prod-db")
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListSnapshots returned %d snapshots, want 2", len(got))
	}
	// Most recent first.
	if got[0].ID != "snap-2" || got[1].ID != "snap-1" {
		t.Errorf("ListSnapshots order = [%s, %s], want [snap-2, snap-1]", got[0].ID, got[1].ID)
	}
	if got[1].Checksum != "sha256:aaa" {
		t.Errorf("Checksum = %q, want sha256:aaa", got[1].Checksum)
	}
	if got[1].JobID != "job-1" {
		t.Errorf("JobID = %q, want job-1", got[1].JobID)
	}
	if !got[1].CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got[1].CreatedAt, now)
	}
}

// TestConcurrentWrites exercises the store from multiple goroutines at
// once. It exists to confirm the SetMaxOpenConns(1) choice in Open: without
// it, a ":memory:" DSN can silently hand out a second, empty in-memory
// database to a second pooled connection, or concurrent writers can trip
// SQLite's "database is locked" error.
func TestConcurrentWrites(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job := core.Job{
				ID:       fmt.Sprintf("job-%d", i),
				Host:     "prod-db",
				Plugin:   "postgres",
				Status:   core.JobStatusQueued,
				QueuedAt: time.Now().UTC(),
			}
			errs <- s.CreateJob(ctx, job)
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("CreateJob: %v", err)
		}
	}

	for i := 0; i < n; i++ {
		if _, err := s.GetJob(ctx, fmt.Sprintf("job-%d", i)); err != nil {
			t.Errorf("GetJob(job-%d): %v", i, err)
		}
	}
}
