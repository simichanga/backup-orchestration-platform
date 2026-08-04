package storage

import (
	"context"
	"errors"
	"io"
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

func (f *fakeRunner) RunToWriter(_ context.Context, w io.Writer, args ...string) error {
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
	if out != nil {
		if _, werr := w.Write(out); werr != nil {
			return werr
		}
	}
	return err
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

func TestRetrieveDumpsRecordedSourcePathToTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "restored-artifact")

	f := &fakeRunner{
		outputs: [][]byte{
			[]byte(`[{"id":"snap-1","hostname":"prod-db","paths":["/tmp/bop/pg-testdb-12345"]}]`),
			[]byte("dumped-content"),
		},
	}
	p := &ResticProvider{run: f}

	if err := p.Retrieve(context.Background(), "snap-1", core.Artifact{Path: targetPath}); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(content) != "dumped-content" {
		t.Errorf("restored content = %q, want %q", content, "dumped-content")
	}

	if len(f.calls) != 2 {
		t.Fatalf("restic invoked %d times, want 2 (snapshot lookup, then dump)", len(f.calls))
	}
	if joined := strings.Join(f.calls[0], " "); !strings.Contains(joined, "snapshots snap-1") {
		t.Errorf("first call args = %q, want a snapshot lookup for snap-1", joined)
	}
	dumpArgs := strings.Join(f.calls[1], " ")
	if !strings.Contains(dumpArgs, "dump snap-1") || !strings.Contains(dumpArgs, "/tmp/bop/pg-testdb-12345") {
		t.Errorf("second call args = %q, want a dump of the snapshot's recorded source path", dumpArgs)
	}
}

func TestResticDumpPathConvertsWindowsDriveLetters(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`C:\Users\SImi\file.txt`, "/C/Users/SImi/file.txt"},
		{`D:\backups\db.dump`, "/D/backups/db.dump"},
		{"/tmp/bop/pg-testdb-123", "/tmp/bop/pg-testdb-123"}, // already unix-style: no-op
		{"", ""},
		{"C", "C"}, // too short to be a drive-letter path
	}
	for _, tt := range tests {
		if got := resticDumpPath(tt.in); got != tt.want {
			t.Errorf("resticDumpPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRetrieveFailsWhenSnapshotHasNoRecordedPath(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte(`[{"id":"snap-1","paths":[]}]`)}}
	p := &ResticProvider{run: f}

	dir := t.TempDir()
	err := p.Retrieve(context.Background(), "snap-1", core.Artifact{Path: filepath.Join(dir, "out")})
	if err == nil {
		t.Fatal("Retrieve: expected an error for a snapshot with no recorded path, got nil")
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

// TestExecRunnerPasswordEnvAuthenticatesAgainstRealRepo proves passwordEnv
// isn't just plumbing: a real restic init/snapshots round-trip authenticates
// correctly when the password is delivered via a named env var (as
// config.yaml's storage.restic.password_env references) rather than a file.
func TestExecRunnerPasswordEnvAuthenticatesAgainstRealRepo(t *testing.T) {
	binary := findRestic(t)
	ctx := context.Background()

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")

	t.Setenv("BOP_TEST_RESTIC_PASSWORD", "test-password-via-env")
	runner := &execRunner{binary: binary, repository: repo, passwordEnv: "BOP_TEST_RESTIC_PASSWORD"}

	if _, err := runner.Run(ctx, "init"); err != nil {
		t.Fatalf("restic init: %v", err)
	}
	if _, err := runner.Run(ctx, "snapshots", "--json"); err != nil {
		t.Fatalf("restic snapshots: %v", err)
	}

	// A wrong password must fail: proves the env var's value is what's
	// actually authenticating, not some other fallback succeeding silently.
	wrongRunner := &execRunner{binary: binary, repository: repo, passwordEnv: "BOP_TEST_RESTIC_WRONG_PASSWORD"}
	t.Setenv("BOP_TEST_RESTIC_WRONG_PASSWORD", "not-the-right-password")
	if _, err := wrongRunner.Run(ctx, "snapshots", "--json"); err == nil {
		t.Fatalf("restic snapshots with wrong password: expected error, got nil")
	}
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

	// Retrieve against a real repository: this is what caught Retrieve's
	// original bug (restic restore --target reconstructs the entire
	// original absolute path under target, rather than writing directly
	// to it) - the fake-runner test alone only proved the (wrong) command
	// string was well-formed, not that the restored content ends up
	// anywhere sensible.
	restoredPath := filepath.Join(dir, "restored-otherdb.dump")
	var otherdbID core.SnapshotID
	for _, s := range snaps {
		if s.Plugin == "postgres" {
			otherdbID = s.ID
		}
	}
	if otherdbID == "" {
		t.Fatalf("could not find a remaining snapshot to retrieve")
	}
	if err := p.Retrieve(ctx, otherdbID, core.Artifact{Path: restoredPath}); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read retrieved file: %v", err)
	}
	// Both myapp (pruned to its latest) and otherdb are "v1"/"v2" content
	// depending on which survived retention; just confirm the retrieved
	// bytes are non-empty and land exactly at restoredPath, not nested
	// under it.
	if len(got) == 0 {
		t.Errorf("retrieved file is empty")
	}
}
