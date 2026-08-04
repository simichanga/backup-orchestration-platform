package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bop/internal/core"
)

// commandRunner abstracts process execution so ResticProvider's argument
// construction and JSON parsing can be unit-tested without a real restic
// binary or repository.
type commandRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// execRunner is the real commandRunner: it shells out to the restic binary.
type execRunner struct {
	binary       string
	repository   string
	passwordFile string
	extraArgs    []string
}

func (r *execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	fullArgs := append([]string{"--repo", r.repository}, args...)
	fullArgs = append(fullArgs, r.extraArgs...)

	cmd := exec.CommandContext(ctx, r.binary, fullArgs...)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD_FILE="+r.passwordFile)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("restic %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ResticProvider implements StorageProvider by shelling out to the restic
// binary. Verified end-to-end against a real restic repository before
// being written: --host scopes ApplyRetention's forget correctly (other
// hosts' snapshots are untouched), and per-resource tags create
// independent retention buckets within the same host (two resources under
// one host+plugin each keep their own N snapshots, not a shared pool).
type ResticProvider struct {
	run commandRunner
}

// NewResticProvider constructs a ResticProvider that shells out to
// binaryPath against repository, authenticating via passwordFile.
// extraArgs are appended to every restic invocation (e.g. --verbose).
func NewResticProvider(binaryPath, repository, passwordFile string, extraArgs []string) *ResticProvider {
	return &ResticProvider{run: &execRunner{
		binary:       binaryPath,
		repository:   repository,
		passwordFile: passwordFile,
		extraArgs:    extraArgs,
	}}
}

// Store tags the snapshot with the artifact's logical identity (host,
// plugin, resource), not restic's default host+local-path grouping: a
// temp file's path is incidental and shouldn't determine retention
// grouping. --group-by host,tags matches this on both backup (--parent
// selection) and ApplyRetention's forget.
func (p *ResticProvider) Store(ctx context.Context, a core.Artifact) (core.SnapshotID, error) {
	args := []string{"backup", a.Path, "--json", "--group-by", "host,tags"}
	if a.Host != "" {
		args = append(args, "--host", a.Host)
	}
	if a.Plugin != "" {
		args = append(args, "--tag", "plugin:"+a.Plugin)
	}
	if a.ResourceID != "" {
		args = append(args, "--tag", "resource:"+a.ResourceID)
	}

	out, err := p.run.Run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("restic backup: %w", err)
	}

	id, err := parseBackupSnapshotID(out)
	if err != nil {
		return "", fmt.Errorf("restic backup: %w", err)
	}
	return id, nil
}

// resticBackupMessage is one line of `restic backup --json`'s
// newline-delimited output. Only the summary line (the last one) carries
// snapshot_id; other lines are progress/status and are skipped.
type resticBackupMessage struct {
	MessageType string `json:"message_type"`
	SnapshotID  string `json:"snapshot_id"`
}

func parseBackupSnapshotID(output []byte) (core.SnapshotID, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var msg resticBackupMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue // progress lines etc.; only the summary line matters
		}
		if msg.MessageType == "summary" {
			if msg.SnapshotID == "" {
				return "", fmt.Errorf("summary message has no snapshot_id")
			}
			return core.SnapshotID(msg.SnapshotID), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan backup output: %w", err)
	}
	return "", fmt.Errorf("no summary message in backup output")
}

func (p *ResticProvider) Retrieve(ctx context.Context, id core.SnapshotID, a core.Artifact) error {
	if _, err := p.run.Run(ctx, "restore", string(id), "--target", a.Path); err != nil {
		return fmt.Errorf("restic restore: %w", err)
	}
	return nil
}

// Verify is deliberately cheap: an existence check, not `restic check`
// (which scans the whole repository). The controller calls Verify once
// per resource, so an O(repo) check here would turn one backup job into a
// full repo scan - the opposite of what the three-tier verification model
// (structural -> storage -> restore-test) intends for this middle tier.
// restic already validates chunk checksums during backup upload itself.
func (p *ResticProvider) Verify(ctx context.Context, id core.SnapshotID) error {
	out, err := p.run.Run(ctx, "snapshots", string(id), "--json")
	if err != nil {
		return fmt.Errorf("restic snapshots: %w", err)
	}
	var snaps []json.RawMessage
	if err := json.Unmarshal(out, &snaps); err != nil {
		return fmt.Errorf("parse restic snapshots output: %w", err)
	}
	if len(snaps) == 0 {
		return fmt.Errorf("snapshot %s not found", id)
	}
	return nil
}

func (p *ResticProvider) Delete(ctx context.Context, id core.SnapshotID) error {
	if _, err := p.run.Run(ctx, "forget", string(id)); err != nil {
		return fmt.Errorf("restic forget: %w", err)
	}
	return nil
}

type resticSnapshot struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	Time     time.Time `json:"time"`
	Tags     []string  `json:"tags"`
}

func (p *ResticProvider) Snapshots(ctx context.Context) ([]core.Snapshot, error) {
	out, err := p.run.Run(ctx, "snapshots", "--json")
	if err != nil {
		return nil, fmt.Errorf("restic snapshots: %w", err)
	}
	var raw []resticSnapshot
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse restic snapshots output: %w", err)
	}

	snaps := make([]core.Snapshot, 0, len(raw))
	for _, r := range raw {
		snaps = append(snaps, core.Snapshot{
			ID:        core.SnapshotID(r.ID),
			Host:      r.Hostname,
			Plugin:    pluginFromTags(r.Tags),
			CreatedAt: r.Time,
		})
	}
	return snaps, nil
}

func pluginFromTags(tags []string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "plugin:"); ok {
			return v
		}
	}
	return ""
}

// ApplyRetention scopes forget to host, verified directly against a real
// restic repository: without --host, forget's keep-policy applies
// repository-wide, across every host. --prune reclaims space from removed
// snapshots immediately rather than deferring to a separate maintenance
// job, which doesn't exist yet in Phase 1 - accepted tradeoff of a slower
// forget call for not needing a second scheduled operation.
func (p *ResticProvider) ApplyRetention(ctx context.Context, host string, policy core.Policy) error {
	args := []string{"forget", "--group-by", "host,tags", "--prune"}
	if host != "" {
		args = append(args, "--host", host)
	}

	var hasPolicy bool
	if policy.Daily > 0 {
		args = append(args, "--keep-daily", strconv.Itoa(policy.Daily))
		hasPolicy = true
	}
	if policy.Weekly > 0 {
		args = append(args, "--keep-weekly", strconv.Itoa(policy.Weekly))
		hasPolicy = true
	}
	if policy.Monthly > 0 {
		args = append(args, "--keep-monthly", strconv.Itoa(policy.Monthly))
		hasPolicy = true
	}
	if policy.Yearly > 0 {
		args = append(args, "--keep-yearly", strconv.Itoa(policy.Yearly))
		hasPolicy = true
	}

	if !hasPolicy {
		// restic itself refuses an unscoped/policy-less forget ("no policy
		// was specified, no snapshots will be removed"), confirmed against
		// the real binary - but failing before shelling out at all is clearer.
		return fmt.Errorf("restic forget: no retention policy specified for host %q", host)
	}

	if _, err := p.run.Run(ctx, args...); err != nil {
		return fmt.Errorf("restic forget: %w", err)
	}
	return nil
}
