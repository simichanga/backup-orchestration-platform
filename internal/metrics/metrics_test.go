package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"bop/internal/events"
)

func newTestPublisher(t *testing.T) *Publisher {
	t.Helper()
	return New(prometheus.NewRegistry())
}

func TestBackupCompletedIncrementsJobsTotalAndObservesDuration(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()
	start := time.Now().UTC()

	p.Publish(ctx, events.Event{Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: start})
	p.Publish(ctx, events.Event{Type: events.TypeBackupCompleted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: start.Add(5 * time.Second)})

	got := testutil.ToFloat64(p.jobsTotal.WithLabelValues("prod-db", "postgres", "completed"))
	if got != 1 {
		t.Errorf("bop_jobs_total{status=completed} = %v, want 1", got)
	}

	if count := testutil.CollectAndCount(p.jobDuration); count != 1 {
		t.Errorf("jobDuration series count = %d, want 1", count)
	}
}

func TestBackupFailedIncrementsJobsTotalWithFailedStatus(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()

	p.Publish(ctx, events.Event{Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now()})
	p.Publish(ctx, events.Event{Type: events.TypeBackupFailed, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now()})

	got := testutil.ToFloat64(p.jobsTotal.WithLabelValues("prod-db", "postgres", "failed"))
	if got != 1 {
		t.Errorf("bop_jobs_total{status=failed} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(p.jobsTotal.WithLabelValues("prod-db", "postgres", "completed")); got != 0 {
		t.Errorf("bop_jobs_total{status=completed} = %v, want 0 (job failed, not completed)", got)
	}
}

func TestCompletionWithoutMatchingStartStillCountsJobButSkipsDuration(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()

	// No BackupStarted published first - must not panic, and must not
	// fabricate a bogus duration observation.
	p.Publish(ctx, events.Event{Type: events.TypeBackupCompleted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now()})

	got := testutil.ToFloat64(p.jobsTotal.WithLabelValues("prod-db", "postgres", "completed"))
	if got != 1 {
		t.Errorf("bop_jobs_total{status=completed} = %v, want 1", got)
	}
	if count := testutil.CollectAndCount(p.jobDuration); count != 0 {
		t.Errorf("jobDuration series count = %d, want 0 (no matching BackupStarted)", count)
	}
}

func TestJobStartsMapDoesNotLeakAcrossJobs(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()

	p.Publish(ctx, events.Event{Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now()})
	p.Publish(ctx, events.Event{Type: events.TypeBackupCompleted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now()})

	p.mu.Lock()
	n := len(p.jobStarts)
	p.mu.Unlock()
	if n != 0 {
		t.Errorf("jobStarts has %d leftover entries after job-1 completed, want 0", n)
	}
}

func TestArtifactCreatedIncrementsCounter(t *testing.T) {
	p := newTestPublisher(t)
	p.Publish(context.Background(), events.Event{Type: events.TypeArtifactCreated, Host: "prod-db", Plugin: "postgres"})

	if got := testutil.ToFloat64(p.artifactsCreatedTotal.WithLabelValues("prod-db", "postgres")); got != 1 {
		t.Errorf("bop_artifacts_created_total = %v, want 1", got)
	}
}

func TestRetentionAppliedIncrementsCounter(t *testing.T) {
	p := newTestPublisher(t)
	p.Publish(context.Background(), events.Event{Type: events.TypeRetentionApplied, Host: "prod-db"})

	if got := testutil.ToFloat64(p.retentionAppliedTotal.WithLabelValues("prod-db")); got != 1 {
		t.Errorf("bop_retention_applied_total = %v, want 1", got)
	}
}

func TestRestoreVerificationCompletedIncrementsCounter(t *testing.T) {
	p := newTestPublisher(t)
	p.Publish(context.Background(), events.Event{Type: events.TypeRestoreVerificationCompleted, Host: "prod-db", Plugin: "filesystem"})

	if got := testutil.ToFloat64(p.restoreVerifiedTotal.WithLabelValues("prod-db", "filesystem")); got != 1 {
		t.Errorf("bop_restore_verifications_completed_total = %v, want 1", got)
	}
}

func TestUnrecognizedEventTypeIsIgnored(t *testing.T) {
	p := newTestPublisher(t)
	// Must not panic on an event type with no metrics mapping.
	p.Publish(context.Background(), events.Event{Type: events.TypePluginDiscoveryStarted, Host: "prod-db", Plugin: "postgres"})
}
