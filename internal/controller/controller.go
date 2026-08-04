// Package controller implements the backup job pipeline documented in
// docs/02-architecture.md's Backup Job Lifecycle: for each job, discover
// resources, back them up, verify, store, record metadata, optionally
// restore-test, and apply retention. The controller never contains
// source-specific logic; it only calls the BackupPlugin and
// StorageProvider ports.
package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin"
	"bop/internal/storage"
)

// PluginFactory constructs a BackupPlugin instance for a specific host,
// given that host's inventory entry, its plugin-specific config, and the
// controller's configured temp directory (controller.temp_dir) for
// writing artifacts. This is the Plugin Engine's resolution seam: for
// Phase 1 a factory just builds an in-process plugin, but the signature
// doesn't change if a later factory shells out to a subprocess instead.
type PluginFactory func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error)

// Controller is the pipeline's driver. It assumes the job it is given has
// already been persisted to the metadata Store as queued by whoever
// produced it (scheduler, CLI) - see internal/queue's Queue contract.
type Controller struct {
	Inventory    *inventory.Inventory
	Metadata     *metadata.Store
	Storage      storage.StorageProvider
	Verification core.Verification // global default
	TempDir      string
	JobTimeout   time.Duration    // 0 means no per-job deadline
	Logger       *slog.Logger     // optional; defaults to slog.Default()
	Events       events.Publisher // optional; defaults to &events.LogPublisher{}

	factories map[string]PluginFactory
}

func New(inv *inventory.Inventory, md *metadata.Store, sp storage.StorageProvider, verification core.Verification, tempDir string, jobTimeout time.Duration) *Controller {
	return &Controller{
		Inventory:    inv,
		Metadata:     md,
		Storage:      sp,
		Verification: verification,
		TempDir:      tempDir,
		JobTimeout:   jobTimeout,
		factories:    make(map[string]PluginFactory),
	}
}

// RegisterPlugin makes a plugin available to the controller under name,
// matching an inventory.yaml server's plugins key.
func (c *Controller) RegisterPlugin(name string, factory PluginFactory) {
	c.factories[name] = factory
}

func (c *Controller) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Controller) events() events.Publisher {
	if c.Events != nil {
		return c.Events
	}
	return &events.LogPublisher{Logger: c.logger()}
}

// publish emits an event, recovering from a panicking Publisher so a
// badly-behaved subscriber can never fail a backup job. This makes "event
// emission is non-fatal" structural, not just a convention callers have to
// remember.
func (c *Controller) publish(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			c.logger().Error("event publisher panicked", "panic", r, "event_type", e.Type)
		}
	}()
	e.Timestamp = time.Now().UTC()
	c.events().Publish(ctx, e)
}

// RunJob executes the documented backup pipeline for job. A returned error
// means the job could not even be started (unknown host or plugin) - the
// job's persisted status is left untouched in that case, since it was
// never picked up. Once the job is marked in_progress, any failure
// transitions it to failed before RunJob returns; it is never left
// in_progress on a live error path (that state is reserved for detecting
// a crashed controller on the next startup).
func (c *Controller) RunJob(ctx context.Context, job core.Job) error {
	srv, ok := c.Inventory.Servers[job.Host]
	if !ok {
		return fmt.Errorf("controller: host %q not found in inventory", job.Host)
	}

	factory, ok := c.factories[job.Plugin]
	if !ok {
		return fmt.Errorf("controller: no plugin registered for %q", job.Plugin)
	}

	if err := c.Metadata.UpdateJobStatus(ctx, job.ID, core.JobStatusInProgress); err != nil {
		return fmt.Errorf("controller: mark job %s in_progress: %w", job.ID, err)
	}
	c.publish(ctx, events.Event{Type: events.TypeBackupStarted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin})

	pipelineCtx := ctx
	if c.JobTimeout > 0 {
		var cancel context.CancelFunc
		pipelineCtx, cancel = context.WithTimeout(ctx, c.JobTimeout)
		defer cancel()
	}

	pipelineErr := c.runPipeline(pipelineCtx, job, srv, factory)

	finalStatus := core.JobStatusCompleted
	if pipelineErr != nil {
		finalStatus = core.JobStatusFailed
	}

	// A fresh, short-lived context, deliberately not pipelineCtx: a job
	// that failed because its own JobTimeout expired must still be able to
	// record that failure. Using the same (already expired) context here
	// would make that write fail too, leaving the job stuck in_progress
	// until the next controller restart notices via FailOrphanedJobs -
	// which exists for crash recovery, not for routine timeouts.
	recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	completionEvent := events.TypeBackupCompleted
	var eventFields map[string]string
	if pipelineErr != nil {
		completionEvent = events.TypeBackupFailed
		eventFields = map[string]string{"error": pipelineErr.Error()}
	}
	c.publish(recordCtx, events.Event{Type: completionEvent, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Fields: eventFields})

	if err := c.Metadata.UpdateJobStatus(recordCtx, job.ID, finalStatus); err != nil {
		if pipelineErr != nil {
			return fmt.Errorf("controller: job %s failed (%w); additionally failed to record failure: %v", job.ID, pipelineErr, err)
		}
		return fmt.Errorf("controller: mark job %s completed: %w", job.ID, err)
	}

	if pipelineErr != nil {
		return fmt.Errorf("controller: job %s failed: %w", job.ID, pipelineErr)
	}
	return nil
}

// runPipeline discovers resources and backs each up independently: one
// resource's failure does not stop the others from being attempted, so a
// single broken database doesn't block backing up its neighbors on the
// same host. The job is reported failed if any resource failed, but every
// resource that did succeed still has its snapshot recorded.
func (c *Controller) runPipeline(ctx context.Context, job core.Job, srv inventory.Server, factory PluginFactory) error {
	p, err := factory(srv, srv.Plugins[job.Plugin], c.TempDir)
	if err != nil {
		return fmt.Errorf("instantiate plugin %q: %w", job.Plugin, err)
	}

	c.publish(ctx, events.Event{Type: events.TypePluginDiscoveryStarted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin})
	resources, err := p.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover on %q: %w", job.Host, err)
	}
	c.publish(ctx, events.Event{
		Type: events.TypePluginDiscoveryCompleted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin,
		Fields: map[string]string{"resource_count": strconv.Itoa(len(resources))},
	})

	verification := resolveVerification(c.Verification, srv.Verification)

	var errs []error
	for _, res := range resources {
		if err := c.backupResource(ctx, job, p, res, verification); err != nil {
			errs = append(errs, fmt.Errorf("resource %q: %w", res.ID, err))
		}
	}

	// Retention is a per-job step, not per-resource: run it once after all
	// resources have been attempted, regardless of individual outcomes.
	if err := c.Storage.ApplyRetention(ctx, job.Host, job.Policy); err != nil {
		errs = append(errs, fmt.Errorf("apply retention: %w", err))
	} else {
		c.publish(ctx, events.Event{Type: events.TypeRetentionApplied, JobID: job.ID, Host: job.Host, Plugin: job.Plugin})
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d error(s): %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// backupResource runs one resource through Backup, Verify, checksum, Store,
// metadata recording, storage-level Verify, and the optional restore test.
func (c *Controller) backupResource(ctx context.Context, job core.Job, p plugin.BackupPlugin, res core.Resource, verification core.Verification) error {
	artifact, err := p.Backup(ctx, res)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// Stamped here, not by the plugin: the plugin doesn't know BOP's
	// inventory concepts, but StorageProvider implementations (ResticProvider)
	// need this identity to tag/scope what they store.
	artifact.Host = job.Host
	artifact.Plugin = job.Plugin

	// Cleanup must run on every path (success, verify failure, store
	// failure, ...) or a long-running controller slowly fills its temp
	// disk. This is the only failure mode here that is silent rather than
	// loud, so it isn't allowed to depend on the happy path.
	defer c.cleanupArtifact(artifact)

	if err := p.Verify(ctx, artifact); err != nil {
		return fmt.Errorf("verify artifact: %w", err)
	}

	checksum, err := checksumFile(artifact.Path)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	artifact.Checksum = checksum

	c.publish(ctx, events.Event{
		Type: events.TypeArtifactCreated, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID,
		Fields: map[string]string{"size": strconv.FormatInt(artifact.Size, 10), "checksum": artifact.Checksum},
	})

	// Encryption is deferred: no key management story exists yet (open
	// decision, see project notes) - artifact.Encrypted stays false.

	c.publish(ctx, events.Event{Type: events.TypeArtifactUploadStarted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID})
	snapshotID, err := c.Storage.Store(ctx, artifact)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	c.publish(ctx, events.Event{
		Type: events.TypeArtifactUploadCompleted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID,
		Fields: map[string]string{"snapshot_id": string(snapshotID)},
	})

	// Verify before recording: a snapshot that fails its integrity check
	// must never appear in ListSnapshots looking like a good one. If this
	// fails, the artifact may still physically exist in the repository,
	// untracked by our metadata - acceptable for Phase 1, since the
	// alternative (recording an unverified snapshot as trustworthy) is worse.
	c.publish(ctx, events.Event{Type: events.TypeRepositoryVerificationStarted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID})
	if err := c.Storage.Verify(ctx, snapshotID); err != nil {
		return fmt.Errorf("storage verify: %w", err)
	}
	c.publish(ctx, events.Event{Type: events.TypeRepositoryVerificationCompleted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID})

	snap := core.Snapshot{
		ID:        snapshotID,
		JobID:     job.ID,
		Host:      job.Host,
		Plugin:    job.Plugin,
		Size:      artifact.Size,
		Checksum:  artifact.Checksum,
		CreatedAt: time.Now().UTC(),
	}
	if err := c.Metadata.RecordSnapshot(ctx, snap); err != nil {
		return fmt.Errorf("record snapshot: %w", err)
	}

	if verification.Enabled {
		// Restore-test target is deliberately scratch-suffixed, never
		// res.ID itself: restoring into the live resource during routine
		// verification would be a data-loss bug. Uses the artifact we
		// already produced (still on disk - cleanup runs via defer after
		// this function returns), not verification.TargetDir: the
		// controller has no business inventing a restore location for a
		// plugin whose "restore target" might be an identifier (a database
		// name) rather than a directory - that decision belongs to each
		// plugin. In practice, the filesystem plugin picks its own fixed
		// scratch location internally (see restoreTestBase in
		// internal/plugin/filesystem) rather than reading TargetDir, so
		// this config field is not currently wired into either plugin's
		// restore-test path - a config.yaml/inventory.yaml value with no
		// effect. Threading it through would mean either encoding it into
		// the artifact or extending BackupPlugin.Restore's signature;
		// deferred until a plugin actually needs it configurable rather
		// than a plugin-chosen default.
		//
		// No plugin currently provisions a scratch database for
		// identifier-style resources (e.g. postgres), so this will fail
		// for those until one does (see project notes) - failing clearly
		// here is correct: silently treating an unrun restore-test as a
		// pass would be false confidence in exactly the property ("can
		// this actually be restored?") the feature exists to prove.
		restoreTarget := artifact
		restoreTarget.ResourceID = res.ID + plugin.RestoreTestSuffix

		c.publish(ctx, events.Event{Type: events.TypeRestoreVerificationStarted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID})
		if err := p.Restore(ctx, restoreTarget); err != nil {
			return fmt.Errorf("restore test: %w", err)
		}
		c.publish(ctx, events.Event{Type: events.TypeRestoreVerificationCompleted, JobID: job.ID, Host: job.Host, Plugin: job.Plugin, Resource: res.ID})
	}

	return nil
}

func (c *Controller) cleanupArtifact(a core.Artifact) {
	if a.Path == "" {
		return
	}
	if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
		c.logger().Warn("cleanup temp artifact failed", "path", a.Path, "error", err)
	}
}

func checksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}
