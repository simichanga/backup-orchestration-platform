package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
)

func openTestStore(t *testing.T) *metadata.Store {
	t.Helper()
	s, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestListHostsHandler(t *testing.T) {
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"prod-db": {
			Host:     "10.0.0.1",
			SSHKey:   "/home/bop/.ssh/id_ed25519",
			Schedule: "0 3 * * *",
			Plugins: map[string]*inventory.PluginConfig{
				"postgres": {Config: map[string]interface{}{"password_env": "PG_BACKUP_PASSWORD"}},
			},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	rec := httptest.NewRecorder()
	listHostsHandler(inv)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var hosts []hostSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &hosts); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(hosts) != 1 || hosts[0].Name != "prod-db" || hosts[0].Host != "10.0.0.1" {
		t.Fatalf("hosts = %+v, want one entry for prod-db/10.0.0.1", hosts)
	}
	if len(hosts[0].Plugins) != 1 || hosts[0].Plugins[0] != "postgres" {
		t.Errorf("Plugins = %v, want [postgres]", hosts[0].Plugins)
	}
	// SSHKey must never appear in the response body - it's a connection
	// secret, not something to expose over the API.
	if strings.Contains(rec.Body.String(), "id_ed25519") {
		t.Error("response body leaked ssh_key path")
	}
}

func TestListJobsHandlerReturnsAllJobs(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	seedJob(t, md, "job-1", "prod-db", "postgres", core.JobStatusCompleted, now)
	seedJob(t, md, "job-2", "prod-db", "postgres", core.JobStatusQueued, now.Add(time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	rec := httptest.NewRecorder()
	listJobsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var jobs []jobSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(jobs))
	}
}

func TestListJobsHandlerFiltersByStatus(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	seedJob(t, md, "job-1", "prod-db", "postgres", core.JobStatusCompleted, now)
	seedJob(t, md, "job-2", "prod-db", "postgres", core.JobStatusQueued, now.Add(time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs?status=queued", nil)
	rec := httptest.NewRecorder()
	listJobsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var jobs []jobSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-2" {
		t.Fatalf("jobs = %+v, want just job-2", jobs)
	}
}

func TestListJobsHandlerRejectsInvalidStatus(t *testing.T) {
	md := openTestStore(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs?status=bogus", nil)
	rec := httptest.NewRecorder()
	listJobsHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGetJobHandler(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	seedJob(t, md, "job-1", "prod-db", "postgres", core.JobStatusCompleted, now)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/job-1", nil)
	req.SetPathValue("id", "job-1")
	rec := httptest.NewRecorder()
	getJobHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var job jobSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if job.ID != "job-1" {
		t.Errorf("job.ID = %q, want job-1", job.ID)
	}
}

func TestGetJobHandlerNotFound(t *testing.T) {
	md := openTestStore(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	getJobHandler(md)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListSnapshotsHandlerRequiresHost(t *testing.T) {
	md := openTestStore(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil)
	rec := httptest.NewRecorder()
	listSnapshotsHandler(md)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestListSnapshotsHandler(t *testing.T) {
	md := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := md.RecordSnapshot(ctx, core.Snapshot{ID: "snap-1", JobID: "job-1", Host: "prod-db", Plugin: "postgres", Size: 100, Checksum: "sha256:aaa", CreatedAt: now}); err != nil {
		t.Fatalf("RecordSnapshot: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/snapshots?host=prod-db", nil)
	rec := httptest.NewRecorder()
	listSnapshotsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var snaps []snapshotSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &snaps); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "snap-1" {
		t.Fatalf("snaps = %+v, want one entry for snap-1", snaps)
	}
}

func TestListEventsHandlerDefaultLimit(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	seedEvent(t, md, "job-1", "prod-db", now)
	seedEvent(t, md, "job-1", "prod-db", now.Add(time.Second))

	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	rec := httptest.NewRecorder()
	listEventsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var evts []eventSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &evts); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("len(evts) = %d, want 2", len(evts))
	}
}

func TestListEventsHandlerFiltersByJobIDAndHost(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	seedEvent(t, md, "job-1", "prod-db", now)
	seedEvent(t, md, "job-2", "other-host", now.Add(time.Second))

	req := httptest.NewRequest(http.MethodGet, "/v1/events?job_id=job-1&host=prod-db", nil)
	rec := httptest.NewRecorder()
	listEventsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var evts []eventSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &evts); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(evts) != 1 || evts[0].JobID != "job-1" {
		t.Fatalf("evts = %+v, want just job-1's event", evts)
	}
}

func TestListEventsHandlerRespectsLimit(t *testing.T) {
	md := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		seedEvent(t, md, "job-1", "prod-db", now.Add(time.Duration(i)*time.Second))
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/events?limit=2", nil)
	rec := httptest.NewRecorder()
	listEventsHandler(md)(rec, req)

	var evts []eventSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &evts); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("len(evts) = %d, want 2", len(evts))
	}
}

func TestListEventsHandlerRejectsNonPositiveLimit(t *testing.T) {
	md := openTestStore(t)
	for _, limit := range []string{"0", "-1", "not-a-number"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/events?limit="+limit, nil)
		rec := httptest.NewRecorder()
		listEventsHandler(md)(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%q: status = %d, want 400", limit, rec.Code)
		}
	}
}

func TestListEventsHandlerClampsLimitToMax(t *testing.T) {
	md := openTestStore(t)
	seedEvent(t, md, "job-1", "prod-db", time.Now().UTC())

	req := httptest.NewRequest(http.MethodGet, "/v1/events?limit=999999", nil)
	rec := httptest.NewRecorder()
	listEventsHandler(md)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (an oversized limit should clamp, not error)", rec.Code)
	}
}

func seedEvent(t *testing.T, md *metadata.Store, jobID, host string, ts time.Time) {
	t.Helper()
	e := events.Event{Type: events.TypeBackupCompleted, JobID: jobID, Host: host, Timestamp: ts}
	if err := md.RecordEvent(context.Background(), e); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
}

func seedJob(t *testing.T, md *metadata.Store, id, host, plugin string, status core.JobStatus, queuedAt time.Time) {
	t.Helper()
	job := core.Job{ID: id, Host: host, Plugin: plugin, Status: status, QueuedAt: queuedAt}
	if err := md.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob(%s): %v", id, err)
	}
}
