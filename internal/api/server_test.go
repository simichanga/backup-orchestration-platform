package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
)

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
	inv := &inventory.Inventory{Servers: map[string]inventory.Server{
		"prod-db": {Host: "10.0.0.1", Plugins: map[string]*inventory.PluginConfig{"postgres": {}}},
	}}

	s, err := NewServer("127.0.0.1:0", []string{"real-token"}, inv, md)
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

	get := func(path, token string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+s.Addr()+path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

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
		resp := get("/v1/hosts", "real-token")
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
		resp := get("/v1/jobs/job-1", "real-token")
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

	t.Run("unknown route is a 404, not an auth bypass", func(t *testing.T) {
		resp := get("/v1/does-not-exist", "real-token")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
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
			resp := get(path, "real-token")
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

	s1, err := NewServer("127.0.0.1:0", []string{"t"}, inv, md)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s1.Shutdown(ctx)
	})

	if _, err := NewServer(s1.Addr(), []string{"t"}, inv, md); err == nil {
		t.Fatal("NewServer: expected an error binding an already-bound address, got nil")
	}
}
