package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"bop/internal/core"
)

// ErrJobNotFound is returned when a job ID has no matching row.
var ErrJobNotFound = errors.New("metadata: job not found")

// CreateJob persists a job. Callers must do this, with the job already
// marked core.JobStatusQueued, before handing it to a Queue - see the
// package doc and internal/queue's Queue contract.
func (s *Store) CreateJob(ctx context.Context, job core.Job) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, host, plugin, status, retention_daily, retention_weekly, retention_monthly, retention_yearly, queued_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Host, job.Plugin, job.Status,
		job.Policy.Daily, job.Policy.Weekly, job.Policy.Monthly, job.Policy.Yearly,
		job.QueuedAt, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("metadata: create job %s: %w", job.ID, err)
	}
	return nil
}

// UpdateJobStatus transitions a job to a new status.
func (s *Store) UpdateJobStatus(ctx context.Context, id string, status core.JobStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("metadata: update job %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("metadata: update job %s: %w", id, err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

// GetJob retrieves a job by ID.
func (s *Store) GetJob(ctx context.Context, id string) (core.Job, error) {
	var job core.Job
	err := s.db.QueryRowContext(ctx, `
		SELECT id, host, plugin, status, retention_daily, retention_weekly, retention_monthly, retention_yearly, queued_at
		FROM jobs WHERE id = ?`, id,
	).Scan(&job.ID, &job.Host, &job.Plugin, &job.Status,
		&job.Policy.Daily, &job.Policy.Weekly, &job.Policy.Monthly, &job.Policy.Yearly,
		&job.QueuedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Job{}, ErrJobNotFound
	}
	if err != nil {
		return core.Job{}, fmt.Errorf("metadata: get job %s: %w", id, err)
	}
	return job, nil
}

// FailOrphanedJobs marks every job still in_progress as failed. Call this
// once on controller startup: per the documented Phase 1 crash-recovery
// default, in-flight jobs from a previous run are not resumed. Returns the
// number of jobs marked failed.
func (s *Store) FailOrphanedJobs(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE status = ?`,
		core.JobStatusFailed, time.Now().UTC(), core.JobStatusInProgress)
	if err != nil {
		return 0, fmt.Errorf("metadata: fail orphaned jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("metadata: fail orphaned jobs: %w", err)
	}
	return int(n), nil
}
