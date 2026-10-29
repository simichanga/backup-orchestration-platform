package logs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bop/internal/core"
)

// fakeExecutor is a sshexec.Executor test double: it answers each command
// by its first matching prefix in out.
type fakeExecutor struct {
	calls []string
	out   map[string]string
	err   error
}

func (f *fakeExecutor) Run(_ context.Context, command string, _ io.Reader, stdout io.Writer) error {
	f.calls = append(f.calls, command)
	if f.err != nil {
		return f.err
	}
	for prefix, out := range f.out {
		if strings.HasPrefix(command, prefix) {
			_, err := io.WriteString(stdout, out)
			return err
		}
	}
	return nil
}

var fixedNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func newPlugin(t *testing.T, exec *fakeExecutor, cfg Config) *Plugin {
	t.Helper()
	if cfg.Window == 0 {
		cfg.Window = defaultWindow
	}
	return &Plugin{exec: exec, cfg: cfg, tempDir: t.TempDir(), now: func() time.Time { return fixedNow }}
}

func TestDiscoverFiltersContainers(t *testing.T) {
	exec := &fakeExecutor{out: map[string]string{
		"'docker' 'ps'": "presa-prod-app\ninformix-ac\nshared-traefik\n$(evil)\n\n",
	}}
	p := newPlugin(t, exec, Config{Journal: true, Containers: []string{"*"}, ExcludeContainers: []string{"informix-*"}})

	resources, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var ids []string
	for _, r := range resources {
		ids = append(ids, r.ID)
	}
	if got, want := strings.Join(ids, ","), "journal,container-presa-prod-app,container-shared-traefik"; got != want {
		t.Errorf("Discover() = %s, want %s (excluded and unsafe names dropped)", got, want)
	}
}

func TestDiscoverJournalOnlyRunsNothingRemote(t *testing.T) {
	exec := &fakeExecutor{}
	p := newPlugin(t, exec, Config{Journal: true})

	resources, err := p.Discover(context.Background())
	if err != nil || len(resources) != 1 || resources[0].ID != "journal" {
		t.Fatalf("Discover() = %+v, %v; want just journal", resources, err)
	}
	if len(exec.calls) != 0 {
		t.Errorf("calls = %q, want none", exec.calls)
	}
}

func TestBackupJournalUsesWindow(t *testing.T) {
	exec := &fakeExecutor{out: map[string]string{"'journalctl'": "line one\nline two\n"}}
	p := newPlugin(t, exec, Config{Journal: true, Window: 72 * time.Hour})

	a, err := p.Backup(context.Background(), core.Resource{ID: "journal"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !strings.Contains(exec.calls[0], "'--since' '2026-10-26 12:00:00 UTC'") {
		t.Errorf("command = %q, want --since 72h before now, in UTC", exec.calls[0])
	}
	if got, _ := os.ReadFile(a.Path); string(got) != "line one\nline two\n" || a.Size != 18 {
		t.Errorf("artifact = %q (size %d)", got, a.Size)
	}
}

func TestBackupContainerMergesStderr(t *testing.T) {
	exec := &fakeExecutor{}
	p := newPlugin(t, exec, Config{Containers: []string{"*"}, Window: time.Hour})

	if _, err := p.Backup(context.Background(), core.Resource{ID: "container-shared-traefik"}); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	want := `'sh' '-c' 'exec docker logs --timestamps --since "$1" "$2" 2>&1' 'sh' '2026-10-29T11:00:00Z' 'shared-traefik'`
	if exec.calls[0] != want {
		t.Errorf("command = %q, want %q", exec.calls[0], want)
	}
}

func TestBackupRejectsUnknownOrUnsafeResource(t *testing.T) {
	p := newPlugin(t, &fakeExecutor{}, Config{Journal: true})
	for _, id := range []string{"something", "container-$(rm -rf ~)"} {
		if _, err := p.Backup(context.Background(), core.Resource{ID: id}); err == nil {
			t.Errorf("Backup(%q) = nil error, want error", id)
		}
	}
}

func TestBackupFailureRemovesArtifact(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("No such container")}
	p := newPlugin(t, exec, Config{Containers: []string{"*"}})

	if _, err := p.Backup(context.Background(), core.Resource{ID: "container-gone"}); err == nil {
		t.Fatal("Backup() = nil error, want error")
	}
	if entries, _ := os.ReadDir(p.tempDir); len(entries) != 0 {
		t.Errorf("temp dir has %d leftover files, want 0", len(entries))
	}
}

func TestVerifyAcceptsEmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Plugin{}).Verify(context.Background(), core.Artifact{Path: path}); err != nil {
		t.Errorf("Verify(empty) = %v, want nil (a quiet window is valid)", err)
	}
	if err := (&Plugin{}).Verify(context.Background(), core.Artifact{Path: path + ".missing"}); err == nil {
		t.Error("Verify(missing) = nil, want error")
	}
}

func TestRestoreAlwaysFails(t *testing.T) {
	err := (&Plugin{}).Restore(context.Background(), core.Artifact{ResourceID: "journal-bop-verify"})
	if err == nil || !strings.Contains(err.Error(), "bop restore") {
		t.Errorf("Restore() = %v, want an error pointing at bop restore", err)
	}
}

func TestHealthChecksConfiguredAccess(t *testing.T) {
	exec := &fakeExecutor{}
	p := newPlugin(t, exec, Config{Journal: true, Containers: []string{"*"}})
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(exec.calls) != 2 || !strings.HasPrefix(exec.calls[0], "journalctl") || !strings.HasPrefix(exec.calls[1], "docker") {
		t.Errorf("calls = %q, want a journal check then a docker check", exec.calls)
	}
}
