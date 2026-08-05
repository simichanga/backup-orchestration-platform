package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"bop/internal/controller"
	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/plugin"
	"bop/internal/queue"
)

// fakePlugin is a minimal plugin.BackupPlugin for tests that only need a
// factory to construct successfully - the API layer's own tests care
// whether ctl.BuildPlugin succeeds, not what a real plugin actually does
// with SSH/postgres/filesystem specifics.
type fakePlugin struct{}

func (fakePlugin) Discover(ctx context.Context) ([]core.Resource, error) { return nil, nil }
func (fakePlugin) Backup(ctx context.Context, r core.Resource) (core.Artifact, error) {
	return core.Artifact{}, nil
}
func (fakePlugin) Restore(ctx context.Context, a core.Artifact) error { return nil }
func (fakePlugin) Verify(ctx context.Context, a core.Artifact) error  { return nil }
func (fakePlugin) Health(ctx context.Context) error                   { return nil }
func (fakePlugin) Metadata() core.PluginMetadata                      { return core.PluginMetadata{Name: "fake"} }

func fakeFactory(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return fakePlugin{}, nil
}

// newTestController builds a real *controller.Controller (needed by
// NewServer since POST /v1/backups validates through it) with a
// fakePlugin factory registered under "postgres" - matching the plugin
// name every fixture inventory in this file uses.
func newTestController(t *testing.T, md *metadata.Store, inv *inventory.Inventory) *controller.Controller {
	t.Helper()
	ctl := controller.New(inv, md, nil, core.Verification{}, t.TempDir(), 0)
	ctl.RegisterPlugin("postgres", fakeFactory)
	return ctl
}

// TestServerEndToEnd drives a real HTTP request over a real socket through
// NewServer's full stack - routing, auth middleware, and a handler reading
// from a real (in-memory) metadata.Store - rather than only exercising
// handlers in isolation via httptest.NewRecorder (see handlers_test.go).
// This is the test that would have caught a routing/auth wiring mistake
// the per-handler tests structurally can't see.
func TestServerEndToEnd(t *testing.T) {
	md := openTestStore(t)
	if err := md.CreateJob(context.Background(), core.Job{
		ID: "job-1", Host: "prod-db", Plugin: "postgres",
		Status: core.JobStatusCompleted, QueuedAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := md.RecordSnapshot(context.Background(), core.Snapshot{
		ID: "snap-1", JobID: "job-1", Host: "prod-db", Plugin: "postgres",
		Size: 100, Checksum: "sha256:aaa", CreatedAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("RecordSnapshot: %v", err)
	}
	if err := md.RecordEvent(context.Background(), events.Event{
		Type: events.TypeBackupCompleted, JobID: "job-1", Host: "prod-db", Plugin: "postgres",
		Timestamp: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"prod-db": {Host: "10.0.0.1", Plugins: map[string]*inventory.PluginConfig{"postgres": {}}},
	}}
	ctl := newTestController(t, md, inv)
	q := queue.NewMemory(16)

	s, err := NewServer("127.0.0.1:0", []string{"read-token"}, []string{"write-token"}, ctl, q)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	})

	client := &http.Client{}

	do := func(method, path, token, body string) *http.Response {
		t.Helper()
		var reqBody io.Reader
		if body != "" {
			reqBody = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, "http://"+s.Addr()+path, reqBody)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	get := func(path, token string) *http.Response { return do(http.MethodGet, path, token, "") }

	t.Run("no token is rejected", func(t *testing.T) {
		resp := get("/v1/hosts", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("wrong token is rejected", func(t *testing.T) {
		resp := get("/v1/hosts", "not-the-token")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("valid token reaches the hosts handler", func(t *testing.T) {
		resp := get("/v1/hosts", "read-token")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var hosts []hostSummary
		if err := json.Unmarshal(body, &hosts); err != nil {
			t.Fatalf("unmarshal: %v (body: %s)", err, body)
		}
		if len(hosts) != 1 || hosts[0].Name != "prod-db" {
			t.Errorf("hosts = %+v, want one entry for prod-db", hosts)
		}
	})

	t.Run("valid token reaches the jobs handler", func(t *testing.T) {
		resp := get("/v1/jobs/job-1", "read-token")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var job jobSummary
		if err := json.Unmarshal(body, &job); err != nil {
			t.Fatalf("unmarshal: %v (body: %s)", err, body)
		}
		if job.ID != "job-1" {
			t.Errorf("job.ID = %q, want job-1", job.ID)
		}
	})

	t.Run("valid token reaches the events handler", func(t *testing.T) {
		resp := get("/v1/events", "read-token")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var evts []eventSummary
		if err := json.Unmarshal(body, &evts); err != nil {
			t.Fatalf("unmarshal: %v (body: %s)", err, body)
		}
		if len(evts) != 1 || evts[0].JobID != "job-1" {
			t.Errorf("events = %+v, want one entry for job-1", evts)
		}
	})

	t.Run("events limit rejects non-positive values", func(t *testing.T) {
		resp := get("/v1/events?limit=0", "read-token")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("unknown route is a 404, not an auth bypass", func(t *testing.T) {
		resp := get("/v1/does-not-exist", "read-token")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("read token cannot trigger a backup", func(t *testing.T) {
		resp := do(http.MethodPost, "/v1/backups", "read-token", `{"host":"prod-db","plugin":"postgres"}`)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401 (a read token must not reach a write endpoint)", resp.StatusCode)
		}
	})

	t.Run("write token triggers a backup", func(t *testing.T) {
		resp := do(http.MethodPost, "/v1/backups", "write-token", `{"host":"prod-db","plugin":"postgres"}`)
		if resp.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 202 (body: %s)", resp.StatusCode, body)
		}
		body, _ := io.ReadAll(resp.Body)
		var job jobSummary
		if err := json.Unmarshal(body, &job); err != nil {
			t.Fatalf("unmarshal: %v (body: %s)", err, body)
		}
		if job.Host != "prod-db" || job.Plugin != "postgres" || job.Status != "queued" {
			t.Errorf("job = %+v, want a queued prod-db/postgres job", job)
		}
		if q.Len() != 1 {
			t.Errorf("queue.Len() = %d, want 1 (the triggered job)", q.Len())
		}
	})

	t.Run("write token rejects an unknown host", func(t *testing.T) {
		resp := do(http.MethodPost, "/v1/backups", "write-token", `{"host":"no-such-host","plugin":"postgres"}`)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("write token rejects a malformed body", func(t *testing.T) {
		resp := do(http.MethodPost, "/v1/backups", "write-token", `not json`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	// Every endpoint's response fields must use the same casing convention
	// (camelCase). core.Job/core.Snapshot have no json tags of their own -
	// see jobSummary/snapshotSummary's doc comment - so this guards against
	// a future handler serializing a core.* type directly and silently
	// reintroducing PascalCase alongside hostSummary's camelCase.
	t.Run("all endpoints use consistent camelCase field names", func(t *testing.T) {
		assertCamelCase := func(t *testing.T, path string, wantKeys ...string) {
			t.Helper()
			resp := get(path, "read-token")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200", path, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			var items []map[string]interface{}
			if err := json.Unmarshal(body, &items); err != nil {
				t.Fatalf("unmarshal %s: %v (body: %s)", path, err, body)
			}
			if len(items) == 0 {
				t.Fatalf("GET %s returned no items to check", path)
			}
			for _, key := range wantKeys {
				if _, ok := items[0][key]; !ok {
					t.Errorf("GET %s: response missing camelCase key %q, got keys %v", path, key, keysOf(items[0]))
				}
			}
		}

		assertCamelCase(t, "/v1/hosts", "name", "host", "plugins", "schedule")
		assertCamelCase(t, "/v1/jobs", "id", "host", "plugin", "status", "queuedAt")
		assertCamelCase(t, "/v1/snapshots?host=prod-db", "id", "jobId", "host", "plugin", "checksum", "createdAt")
		assertCamelCase(t, "/v1/events", "type", "jobId", "host", "plugin", "resource", "fields", "timestamp")
	})
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestNewServerFailsFastOnPortInUse(t *testing.T) {
	md := openTestStore(t)
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{}}
	ctl := newTestController(t, md, inv)
	q := queue.NewMemory(16)

	s1, err := NewServer("127.0.0.1:0", []string{"t"}, nil, ctl, q)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s1.Shutdown(ctx)
	})

	if _, err := NewServer(s1.Addr(), []string{"t"}, nil, ctl, q); err == nil {
		t.Fatal("NewServer: expected an error binding an already-bound address, got nil")
	}
}
