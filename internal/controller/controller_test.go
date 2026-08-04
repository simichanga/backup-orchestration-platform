package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin"
)

// stubPlugin is a test double for plugin.BackupPlugin. Backup writes a real
// temp file so the controller's checksum step has something to hash, and
// so cleanup-on-every-path can be asserted against the filesystem.
type stubPlugin struct {
	resources  []core.Resource
	backupErr  map[string]error // per-resource ID; nil entries mean success
	verifyErr  error
	restoreErr error
	tempDir    string

	// blockUntilCtxDone makes Backup block on ctx instead of returning
	// immediately, to exercise JobTimeout cancellation.
	blockUntilCtxDone bool

	restoreCalls int
}

func (p *stubPlugin) Discover(context.Context) ([]core.Resource, error) { return p.resources, nil }

func (p *stubPlugin) Backup(ctx context.Context, res core.Resource) (core.Artifact, error) {
	if p.blockUntilCtxDone {
		<-ctx.Done()
		return core.Artifact{}, ctx.Err()
	}
	if err := p.backupErr[res.ID]; err != nil {
		return core.Artifact{}, err
	}
	path := filepath.Join(p.tempDir, "artifact-"+res.ID)
	content := []byte("dummy-data-" + res.ID)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return core.Artifact{}, err
	}
	return core.Artifact{ResourceID: res.ID, Path: path, Size: int64(len(content)), CreatedAt: time.Now()}, nil
}

func (p *stubPlugin) Restore(context.Context, core.Artifact) error {
	p.restoreCalls++
	return p.restoreErr
}

func (p *stubPlugin) Verify(context.Context, core.Artifact) error { return p.verifyErr }
func (p *stubPlugin) Health(context.Context) error                { return nil }
func (p *stubPlugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "stub", Version: "test"}
}

// stubStorage is a test double for storage.StorageProvider.
type stubStorage struct {
	storeErr     error
	verifyErr    error
	retentionErr error

	stored        map[core.SnapshotID]core.Artifact
	retentionCall int
	retentionHost string
}

func newStubStorage() *stubStorage {
	return &stubStorage{stored: make(map[core.SnapshotID]core.Artifact)}
}

func (s *stubStorage) Store(_ context.Context, a core.Artifact) (core.SnapshotID, error) {
	if s.storeErr != nil {
		return "", s.storeErr
	}
	id := core.SnapshotID("snap-" + a.ResourceID)
	s.stored[id] = a
	return id, nil
}

func (s *stubStorage) Retrieve(context.Context, core.SnapshotID, core.Artifact) error { return nil }
func (s *stubStorage) Verify(context.Context, core.SnapshotID) error                  { return s.verifyErr }
func (s *stubStorage) Delete(context.Context, core.SnapshotID) error                  { return nil }
func (s *stubStorage) Snapshots(context.Context) ([]core.Snapshot, error)             { return nil, nil }
func (s *stubStorage) ApplyRetention(_ context.Context, host string, _ core.Policy) error {
	s.retentionCall++
	s.retentionHost = host
	return s.retentionErr
}

func testInventory(verification *core.Verification) *inventory.Inventory {
	return &inventory.Inventory{
		Servers: map[string]inventory.Server{
			"prod-db": {
				Host:         "192.168.1.100",
				Plugins:      map[string]*inventory.PluginConfig{"postgres": nil},
				Retention:    core.Policy{Daily: 7},
				Verification: verification,
			},
		},
	}
}

func setup(t *testing.T, p *stubPlugin, s *stubStorage, inv *inventory.Inventory) (*Controller, *metadata.Store) {
	t.Helper()
	md, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { md.Close() })

	c := New(inv, md, s, core.Verification{Enabled: false}, t.TempDir(), 0)
	c.RegisterPlugin("postgres", func(inventory.Server, *inventory.PluginConfig) (plugin.BackupPlugin, error) {
		return p, nil
	})
	return c, md
}

func TestRunJobSuccess(t *testing.T) {
	dir := t.TempDir()
	p := &stubPlugin{
		resources: []core.Resource{{ID: "myapp"}},
		backupErr: map[string]error{},
		tempDir:   dir,
	}
	s := newStubStorage()
	c, md := setup(t, p, s, testInventory(nil))
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, Policy: core.Policy{Daily: 7}, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := c.RunJob(ctx, job); err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	got, err := md.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusCompleted {
		t.Errorf("job status = %q, want completed", got.Status)
	}

	snaps, err := md.ListSnapshots(ctx, "prod-db")
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("ListSnapshots returned %d, want 1", len(snaps))
	}
	if snaps[0].Checksum == "" {
		t.Errorf("snapshot checksum is empty")
	}

	if s.retentionCall != 1 {
		t.Errorf("ApplyRetention called %d times, want 1", s.retentionCall)
	}
	if s.retentionHost != "prod-db" {
		t.Errorf("ApplyRetention host = %q, want prod-db (must be scoped to the job's host)", s.retentionHost)
	}

	// Temp artifact must be cleaned up after a successful run.
	if _, err := os.Stat(filepath.Join(dir, "artifact-myapp")); !os.IsNotExist(err) {
		t.Errorf("temp artifact still exists after successful run: %v", err)
	}
}

func TestRunJobPartialResourceFailure(t *testing.T) {
	dir := t.TempDir()
	p := &stubPlugin{
		resources: []core.Resource{{ID: "good"}, {ID: "bad"}},
		backupErr: map[string]error{"bad": errors.New("dump failed")},
		tempDir:   dir,
	}
	s := newStubStorage()
	c, md := setup(t, p, s, testInventory(nil))
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, Policy: core.Policy{Daily: 7}, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	err := c.RunJob(ctx, job)
	if err == nil {
		t.Fatalf("RunJob: expected error from failed resource, got nil")
	}

	got, err := md.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusFailed {
		t.Errorf("job status = %q, want failed", got.Status)
	}

	// The resource that succeeded must still have its snapshot recorded -
	// one broken resource must not lose progress on the others.
	snaps, err := md.ListSnapshots(ctx, "prod-db")
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "snap-good" {
		t.Errorf("ListSnapshots = %+v, want one snapshot for the good resource", snaps)
	}

	if s.retentionCall != 1 {
		t.Errorf("ApplyRetention called %d times, want 1 (once per job, regardless of resource failures)", s.retentionCall)
	}
}

func TestRunJobCleansUpArtifactOnMidPipelineFailure(t *testing.T) {
	dir := t.TempDir()
	p := &stubPlugin{
		resources: []core.Resource{{ID: "myapp"}},
		backupErr: map[string]error{},
		verifyErr: errors.New("corrupt dump"),
		tempDir:   dir,
	}
	s := newStubStorage()
	c, md := setup(t, p, s, testInventory(nil))
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := c.RunJob(ctx, job); err == nil {
		t.Fatalf("RunJob: expected error from plugin.Verify failure, got nil")
	}

	got, err := md.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusFailed {
		t.Errorf("job status = %q, want failed (must not be left in_progress)", got.Status)
	}

	if _, err := os.Stat(filepath.Join(dir, "artifact-myapp")); !os.IsNotExist(err) {
		t.Errorf("temp artifact still exists after a mid-pipeline failure: %v", err)
	}
}

func TestRunJobUnknownHostLeavesJobUntouched(t *testing.T) {
	p := &stubPlugin{tempDir: t.TempDir()}
	s := newStubStorage()
	c, md := setup(t, p, s, testInventory(nil))
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "no-such-host", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := c.RunJob(ctx, job); err == nil {
		t.Fatalf("RunJob: expected error for unknown host, got nil")
	}

	got, err := md.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusQueued {
		t.Errorf("job status = %q, want unchanged (queued) since the job never started", got.Status)
	}
}

func TestRunJobUsesPerHostVerificationOverride(t *testing.T) {
	dir := t.TempDir()
	p := &stubPlugin{resources: []core.Resource{{ID: "myapp"}}, backupErr: map[string]error{}, tempDir: dir}
	s := newStubStorage()
	override := &core.Verification{Enabled: true, TargetDir: filepath.Join(dir, "restore-test")}
	c, md := setup(t, p, s, testInventory(override))
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := c.RunJob(ctx, job); err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if p.restoreCalls != 1 {
		t.Errorf("plugin.Restore called %d times, want 1 (per-host verification override was enabled)", p.restoreCalls)
	}
}

func TestRunJobRespectsJobTimeout(t *testing.T) {
	p := &stubPlugin{
		resources:         []core.Resource{{ID: "myapp"}},
		blockUntilCtxDone: true,
		tempDir:           t.TempDir(),
	}
	s := newStubStorage()
	c, md := setup(t, p, s, testInventory(nil))
	c.JobTimeout = 20 * time.Millisecond
	ctx := context.Background()

	job := core.Job{ID: "job-1", Host: "prod-db", Plugin: "postgres", Status: core.JobStatusQueued, QueuedAt: time.Now().UTC()}
	if err := md.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	start := time.Now()
	err := c.RunJob(ctx, job)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("RunJob: expected an error from job_timeout expiring, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("RunJob error = %v, want context.DeadlineExceeded in the chain", err)
	}
	if elapsed > time.Second {
		t.Errorf("RunJob took %v, want it to return promptly after JobTimeout (20ms) - the plugin call was not actually cancelled", elapsed)
	}

	got, err := md.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != core.JobStatusFailed {
		t.Errorf("job status = %q, want failed", got.Status)
	}
}
