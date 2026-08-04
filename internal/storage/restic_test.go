package storage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bop/internal/core"
)

// fakeRunner is a commandRunner test double: it records every call's args
// and replays canned responses, so arg construction and JSON parsing can
// be tested without a real restic binary or repository.
type fakeRunner struct {
	calls   [][]string
	outputs [][]byte
	errs    []error
	i       int
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	var out []byte
	var err error
	if f.i < len(f.outputs) {
		out = f.outputs[f.i]
	}
	if f.i < len(f.errs) {
		err = f.errs[f.i]
	}
	f.i++
	return out, err
}

func (f *fakeRunner) lastArgs() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// realBackupJSON is a captured sample of `restic backup --json` output
// (status lines followed by the summary line), used to test that
// parseBackupSnapshotID correctly ignores progress lines and reads
// snapshot_id off only the message_type=summary line.
const realBackupJSON = `{"message_type":"status","percent_done":1,"total_files":1,"files_done":1,"total_bytes":10,"bytes_done":10}
{"message_type":"summary","files_new":1,"files_changed":0,"files_unmodified":0,"dirs_new":1,"dirs_changed":0,"dirs_unmodified":0,"data_blobs":1,"tree_blobs":2,"data_added":1363,"data_added_packed":1088,"total_files_processed":1,"total_bytes_processed":10,"total_duration":0.68,"backup_start":"2026-08-04T17:21:58Z","backup_end":"2026-08-04T17:21:59Z","snapshot_id":"ac05c63e23d4ea9c4d8ec49e59ddba33b82015d86b99e9ba928f32e87aeccf95"}
`

func TestStoreTagsAndParsesSnapshotID(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte(realBackupJSON)}}
	p := &ResticProvider{run: f}

	artifact := core.Artifact{ResourceID: "myapp", Host: "prod-db", Plugin: "postgres", Path: "/tmp/artifact"}
	id, err := p.Store(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if id != "ac05c63e23d4ea9c4d8ec49e59ddba33b82015d86b99e9ba928f32e87aeccf95" {
		t.Errorf("Store snapshot ID = %q, unexpected", id)
	}

	args := f.lastArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"--host prod-db", "--tag plugin:postgres", "--tag resource:myapp", "--group-by host,tags"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Store args = %q, missing %q", joined, want)
		}
	}
}

func TestStorePropagatesRunError(t *testing.T) {
	f := &fakeRunner{errs: []error{errors.New("repo locked")}}
	p := &ResticProvider{run: f}

	_, err := p.Store(context.Background(), core.Artifact{Path: "/tmp/x"})
	if err == nil {
		t.Fatalf("Store: expected error, got nil")
	}
}

func TestApplyRetentionScopesHostAndKeepFlags(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{nil}}
	p := &ResticProvider{run: f}

	err := p.ApplyRetention(context.Background(), "prod-db", core.Policy{Daily: 7, Weekly: 4})
	if err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}

	joined := strings.Join(f.lastArgs(), " ")
	for _, want := range []string{"forget", "--host prod-db", "--group-by host,tags", "--keep-daily 7", "--keep-weekly 4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ApplyRetention args = %q, missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "--keep-monthly") || strings.Contains(joined, "--keep-yearly") {
		t.Errorf("ApplyRetention args = %q, should not set keep flags for zero-value policy fields", joined)
	}
}

func TestApplyRetentionRefusesEmptyPolicy(t *testing.T) {
	f := &fakeRunner{}
	p := &ResticProvider{run: f}

	err := p.ApplyRetention(context.Background(), "prod-db", core.Policy{})
	if err == nil {
		t.Fatalf("ApplyRetention: expected error for empty policy, got nil")
	}
	if len(f.calls) != 0 {
		t.Errorf("ApplyRetention called restic %d times for an empty policy, want 0 (must fail before shelling out)", len(f.calls))
	}
}

func TestVerifyExistingSnapshot(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte(`[{"id":"abc123"}]`)}}
	p := &ResticProvider{run: f}

	if err := p.Verify(context.Background(), "abc123"); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifyMissingSnapshot(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte(`[]`)}}
	p := &ResticProvider{run: f}

	if err := p.Verify(context.Background(), "missing"); err == nil {
		t.Errorf("Verify: expected error for missing snapshot, got nil")
	}
}

func TestSnapshotsParsesHostAndPluginTag(t *testing.T) {
	sample := `[{"id":"abc123","hostname":"prod-db","time":"2026-08-04T03:00:00Z","tags":["plugin:postgres","resource:myapp"]}]`
	f := &fakeRunner{outputs: [][]byte{[]byte(sample)}}
	p := &ResticProvider{run: f}

	snaps, err := p.Snapshots(context.Background())
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("Snapshots returned %d, want 1", len(snaps))
	}
	if snaps[0].Host != "prod-db" || snaps[0].Plugin != "postgres" {
		t.Errorf("Snapshots[0] = %+v, want host=prod-db plugin=postgres", snaps[0])
	}
}

func TestDeleteCallsForgetWithID(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{nil}}
	p := &ResticProvider{run: f}

	if err := p.Delete(context.Background(), "snap-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := f.lastArgs()
	if len(got) < 2 || got[0] != "forget" || got[1] != "snap-1" {
		t.Errorf("Delete args = %v, want [forget snap-1 ...]", got)
	}
}

func TestRetrieveCallsRestoreWithTarget(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{nil}}
	p := &ResticProvider{run: f}

	if err := p.Retrieve(context.Background(), "snap-1", core.Artifact{Path: "/tmp/restored"}); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	joined := strings.Join(f.lastArgs(), " ")
	if !strings.Contains(joined, "restore snap-1") || !strings.Contains(joined, "--target /tmp/restored") {
		t.Errorf("Retrieve args = %q, unexpected", joined)
	}
}

// findRestic locates the restic binary for the integration test below,
// skipping the test if it isn't available - the suite must stay green on
// a machine without restic installed. Checks PATH first (correct in CI
// and once a new terminal picks up this dev machine's updated user PATH),
// then falls back to the known install location from this session.
func findRestic(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("restic"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "bin", "restic.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("restic binary not found; skipping real-binary integration test")
	return ""
}

// TestResticProviderIntegration exercises ResticProvider against a real,
// throwaway restic repository. It is the test that actually proves the
// --host/--tag/--group-by design: two resources on the same host must
// each keep their own retention window, not share one pool.
func TestResticProviderIntegration(t *testing.T) {
	binary := findRestic(t)
	ctx := context.Background()

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	passwordFile := filepath.Join(dir, "password.txt")
	if err := os.WriteFile(passwordFile, []byte("test-password"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	runner := &execRunner{binary: binary, repository: repo, passwordFile: passwordFile}
	if _, err := runner.Run(ctx, "init"); err != nil {
		t.Fatalf("restic init: %v", err)
	}
	p := &ResticProvider{run: runner}

	myappPath := filepath.Join(dir, "myapp.dump")
	otherdbPath := filepath.Join(dir, "otherdb.dump")

	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	write(myappPath, "myapp v1")
	id, err := p.Store(ctx, core.Artifact{ResourceID: "myapp", Host: "prod-db", Plugin: "postgres", Path: myappPath})
	if err != nil {
		t.Fatalf("Store myapp v1: %v", err)
	}
	if err := p.Verify(ctx, id); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	write(myappPath, "myapp v2")
	if _, err := p.Store(ctx, core.Artifact{ResourceID: "myapp", Host: "prod-db", Plugin: "postgres", Path: myappPath}); err != nil {
		t.Fatalf("Store myapp v2: %v", err)
	}

	write(otherdbPath, "otherdb v1")
	if _, err := p.Store(ctx, core.Artifact{ResourceID: "otherdb", Host: "prod-db", Plugin: "postgres", Path: otherdbPath}); err != nil {
		t.Fatalf("Store otherdb: %v", err)
	}

	snaps, err := p.Snapshots(ctx)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("Snapshots before retention = %d, want 3", len(snaps))
	}

	if err := p.ApplyRetention(ctx, "prod-db", core.Policy{Daily: 1}); err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}

	snaps, err = p.Snapshots(ctx)
	if err != nil {
		t.Fatalf("Snapshots after retention: %v", err)
	}
	// myapp's 2 snapshots -> pruned to 1; otherdb's 1 -> kept, in its own
	// bucket. If this were 1 (a shared pool) or 3 (host filter didn't
	// apply), the tagging/grouping design would be wrong.
	if len(snaps) != 2 {
		t.Fatalf("Snapshots after retention = %d, want 2 (myapp pruned to 1, otherdb kept independently)", len(snaps))
	}
}
