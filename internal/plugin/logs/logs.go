// Package logs implements the BackupPlugin port for host logs: the
// systemd journal (the source /var/log/syslog is written from) and Docker
// container logs. Connects over SSH like the postgres/mariadb plugins and
// runs journalctl / docker logs there.
//
// Each run captures a rolling window (config.window, default 72h) rather
// than tracking a cursor: stateless, and gap-free as long as fewer runs
// are missed in a row than the window spans. Overlap between consecutive
// snapshots is deduplicated by restic.
//
// Logs are backed up, not restored: Restore always fails, and `bop
// restore --target <file>` extracts a snapshot's log file for reading.
package logs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/plugin"
	"bop/internal/sshexec"
)

const (
	journalResource         = "journal"
	containerResourcePrefix = "container-"
)

// containerName matches Docker's own container naming rule. Discovered
// names come from `docker ps` on the target and end up in shell commands
// and temp file names, so anything else is skipped rather than trusted.
var containerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

type Plugin struct {
	exec    sshexec.Executor
	cfg     Config
	tempDir string
	now     func() time.Time
}

// NewFactory returns a controller.PluginFactory-compatible factory that
// builds a logs Plugin from a server's inventory entry and this plugin's
// config block. knownHostsFile is config.yaml's ssh.known_hosts_file.
func NewFactory(knownHostsFile string) func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
		lCfg, err := ParseConfig(cfg)
		if err != nil {
			return nil, err
		}
		if srv.Host == "" {
			return nil, fmt.Errorf("logs: server host is required")
		}
		if srv.SSHUser == "" || srv.SSHKey == "" {
			return nil, fmt.Errorf("logs: ssh_user and ssh_key are required")
		}

		return &Plugin{
			exec:    &sshexec.SSHExecutor{Addr: srv.Host + ":22", User: srv.SSHUser, KeyPath: srv.SSHKey, KnownHostsFile: knownHostsFile},
			cfg:     lCfg,
			tempDir: tempDir,
			now:     time.Now,
		}, nil
	}
}

// Discover returns the journal (if enabled) plus one resource per
// container matching config.containers and not config.exclude_containers,
// listed fresh from the target on every run.
func (p *Plugin) Discover(ctx context.Context) ([]core.Resource, error) {
	var resources []core.Resource
	if p.cfg.Journal {
		resources = append(resources, core.Resource{ID: journalResource, Name: "systemd journal"})
	}
	if len(p.cfg.Containers) == 0 {
		return resources, nil
	}

	var out bytes.Buffer
	if err := p.exec.Run(ctx, listContainersCommand(), nil, &out); err != nil {
		return nil, fmt.Errorf("logs: list containers: %w", err)
	}
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if name == "" || !containerName.MatchString(name) {
			continue
		}
		if matchesAny(p.cfg.Containers, name) && !matchesAny(p.cfg.ExcludeContainers, name) {
			resources = append(resources, core.Resource{ID: containerResourcePrefix + name, Name: name})
		}
	}
	return resources, sc.Err()
}

func matchesAny(patterns []string, name string) bool {
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

func (p *Plugin) Backup(ctx context.Context, res core.Resource) (core.Artifact, error) {
	since := p.now().Add(-p.cfg.Window)
	var command string
	switch {
	case res.ID == journalResource:
		command = journalCommand(since)
	case strings.HasPrefix(res.ID, containerResourcePrefix) && containerName.MatchString(strings.TrimPrefix(res.ID, containerResourcePrefix)):
		command = containerLogsCommand(strings.TrimPrefix(res.ID, containerResourcePrefix), since)
	default:
		return core.Artifact{}, fmt.Errorf("logs: unknown resource %q", res.ID)
	}

	f, err := os.CreateTemp(p.tempDir, "logs-"+res.ID+"-*")
	if err != nil {
		return core.Artifact{}, fmt.Errorf("logs: create artifact file: %w", err)
	}
	path := f.Name()

	// Close before Remove on every error path - see the postgres plugin.
	if err := p.exec.Run(ctx, command, nil, f); err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("logs: dump %s: %w", res.ID, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("logs: stat artifact: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("logs: close artifact: %w", err)
	}

	return core.Artifact{
		ResourceID: res.ID,
		Path:       path,
		Size:       info.Size(),
		CreatedAt:  time.Now(),
	}, nil
}

// Restore always fails: there is no meaningful way to write logs back
// into a live journal or container. The error points at how to read a
// backed-up log instead, and a restore-test (verification.enabled) on a
// logs server fails loudly rather than pretending to have verified
// anything.
func (p *Plugin) Restore(context.Context, core.Artifact) error {
	return fmt.Errorf("logs: logs cannot be restored in place - extract a snapshot to a file with `bop restore --snapshot <id> --target <file>`; do not enable verification on a server running the logs plugin")
}

// Verify only checks the artifact exists. An empty log is valid: an idle
// container, or a quiet journal, legitimately writes nothing in a window.
// A failed dump is already caught by the remote command's exit status.
func (p *Plugin) Verify(_ context.Context, a core.Artifact) error {
	if _, err := os.Stat(a.Path); err != nil {
		return fmt.Errorf("logs: verify: stat artifact: %w", err)
	}
	return nil
}

// Health checks the SSH user can actually read what is configured, not
// just that SSH works: journal access and docker access are group
// memberships that are easy to miss on a new host.
func (p *Plugin) Health(ctx context.Context) error {
	if p.cfg.Journal {
		if err := p.exec.Run(ctx, "journalctl -q -n 0 --no-pager", nil, io.Discard); err != nil {
			return fmt.Errorf("logs: health check: journal not readable (is the SSH user in systemd-journal?): %w", err)
		}
	}
	if len(p.cfg.Containers) > 0 {
		if err := p.exec.Run(ctx, "docker ps -q", nil, io.Discard); err != nil {
			return fmt.Errorf("logs: health check: docker not usable (is the SSH user in the docker group?): %w", err)
		}
	}
	return nil
}

func (p *Plugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "logs", Version: "0.1.0"}
}
