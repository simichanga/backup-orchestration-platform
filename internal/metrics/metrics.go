// Package metrics exposes Prometheus metrics derived from the same event
// stream the controller already emits (see internal/events), fulfilling
// the "Metrics/Events" step in the documented core backup pipeline
// (docs/resources/scalability-model.png), which is present at every phase
// - including Phase 1, not deferred to a later one.
package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"bop/internal/events"
)

// Publisher is an events.Publisher that records Prometheus metrics rather
// than logging or storage. Wiring it into the same events.Multi fan-out as
// LogPublisher means metrics require zero extra instrumentation calls
// inside the controller or plugins - exactly the "zero extra engineering"
// property the event system already gives logging.
type Publisher struct {
	jobsTotal             *prometheus.CounterVec
	jobDuration           *prometheus.HistogramVec
	artifactsCreatedTotal *prometheus.CounterVec
	retentionAppliedTotal *prometheus.CounterVec
	restoreVerifiedTotal  *prometheus.CounterVec

	mu        sync.Mutex
	jobStarts map[string]time.Time // job ID -> BackupStarted event timestamp
}

// New builds a Publisher and registers its metrics with reg. Callers
// should pass a dedicated *prometheus.Registry (not the global default) so
// multiple Publishers - e.g. one per test, or one per buildApp call in a
// process that never runs "bop controller" - never collide on duplicate
// registration.
func New(reg prometheus.Registerer) *Publisher {
	p := &Publisher{
		jobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bop_jobs_total",
			Help: "Total backup jobs processed, by host, plugin, and final status.",
		}, []string{"host", "plugin", "status"}),
		jobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bop_job_duration_seconds",
			Help:    "Backup job duration in seconds, from BackupStarted to job completion.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1s .. ~34min
		}, []string{"host", "plugin"}),
		artifactsCreatedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bop_artifacts_created_total",
			Help: "Total artifacts successfully backed up, plugin-verified, and checksummed, by host and plugin.",
		}, []string{"host", "plugin"}),
		retentionAppliedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bop_retention_applied_total",
			Help: "Total successful retention policy applications, by host.",
		}, []string{"host"}),
		restoreVerifiedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bop_restore_verifications_completed_total",
			Help: "Total successful restore-test verifications, by host and plugin.",
		}, []string{"host", "plugin"}),
		jobStarts: make(map[string]time.Time),
	}
	reg.MustRegister(p.jobsTotal, p.jobDuration, p.artifactsCreatedTotal, p.retentionAppliedTotal, p.restoreVerifiedTotal)
	return p
}

func (p *Publisher) Publish(_ context.Context, e events.Event) {
	switch e.Type {
	case events.TypeBackupStarted:
		// jobStarts is bounded by concurrently in-flight jobs (at most a
		// handful in Phase 1's single-serial-consumer model): RunJob always
		// reaches its final publish call via a fresh, uncancellable
		// recordCtx (see controller.RunJob), so every BackupStarted is
		// reliably followed by exactly one BackupCompleted/BackupFailed
		// within the same process - no unbounded leak.
		p.mu.Lock()
		p.jobStarts[e.JobID] = e.Timestamp
		p.mu.Unlock()

	case events.TypeBackupCompleted, events.TypeBackupFailed:
		status := "completed"
		if e.Type == events.TypeBackupFailed {
			status = "failed"
		}
		p.jobsTotal.WithLabelValues(e.Host, e.Plugin, status).Inc()

		p.mu.Lock()
		start, ok := p.jobStarts[e.JobID]
		delete(p.jobStarts, e.JobID)
		p.mu.Unlock()
		if ok {
			p.jobDuration.WithLabelValues(e.Host, e.Plugin).Observe(e.Timestamp.Sub(start).Seconds())
		}

	case events.TypeArtifactCreated:
		p.artifactsCreatedTotal.WithLabelValues(e.Host, e.Plugin).Inc()

	case events.TypeRetentionApplied:
		p.retentionAppliedTotal.WithLabelValues(e.Host).Inc()

	case events.TypeRestoreVerificationCompleted:
		p.restoreVerifiedTotal.WithLabelValues(e.Host, e.Plugin).Inc()
	}
}
