# Writing a Plugin

A plugin implements `BackupPlugin` (`internal/plugin/plugin.go`): the
contract every backup source (PostgreSQL, filesystem, ...) implements. The
controller never contains source-specific logic - it only calls these six
methods. This page walks through the contract using the two real,
real-infrastructure-tested implementations (`internal/plugin/postgres`,
`internal/plugin/filesystem`) as worked examples.

## Phase 1: Compiled In, Not Loaded

Plugins ship compiled into the `bop` binary and are registered in-process
in `internal/cli/wiring.go`'s `buildApp`:

```go
ctl.RegisterPlugin("postgres", postgres.NewFactory(cfg.SSH.KnownHostsFile))
ctl.RegisterPlugin("filesystem", filesystem.NewFactory(cfg.SSH.KnownHostsFile))
```

There is no out-of-process plugin loading, no `plugins.dir` scanning, and
no plugin SDK versioning story yet - `config.yaml`'s `plugins.dir` /
`plugins.allow_unsigned` keys exist for a future out-of-tree registry, but
have no effect today. Writing a new built-in plugin today means adding a
package under `internal/plugin/<name>` and a `RegisterPlugin` call in
`wiring.go` - there is currently no way to add a plugin without modifying
and recompiling `bop` itself.

## The Interface

```go
type BackupPlugin interface {
    Discover(ctx context.Context) ([]core.Resource, error)
    Backup(ctx context.Context, resource core.Resource) (core.Artifact, error)
    Restore(ctx context.Context, artifact core.Artifact) error
    Verify(ctx context.Context, artifact core.Artifact) error
    Health(ctx context.Context) error
    Metadata() core.PluginMetadata
}
```

Every method except `Metadata` takes a `context.Context`: these do I/O
(SSH connections, subprocess execution, database queries) that must be
cancellable, notably by `controller.job_timeout`.

### `Discover`

Lists the resources this plugin can back up on its target host. Called
once per job, before any `Backup` call. Each `core.Resource{ID, Name,
Labels}` becomes one independent pipeline run - one resource failing
doesn't stop the others (see the controller's `runPipeline`).

- `postgres.Discover` returns one resource per configured database name.
- `filesystem.Discover` returns one resource per configured path.

Neither plugin does anything dynamic here (no live enumeration against the
target) - both just echo back their static config. A plugin for a source
with a variable resource set (e.g. "every database on the server", "every
Docker volume") would do real discovery here instead.

### `Backup`

Produces a `core.Artifact` from a single resource - typically a local temp
file the controller will checksum, upload, and clean up. Both built-in
plugins follow the same shape:

1. `os.CreateTemp(tempDir, ...)` for a local scratch file.
2. Run the actual backup command (SSH-executed remotely, output streamed
   to the local file).
3. **Close the file before removing it on any error path, not after via
   `defer`.** POSIX allows unlinking an open file; Windows does not. Both
   plugins hit this during development - a test that ran fine on the
   original dev machine failed on a differently-behaved filesystem. Always
   `f.Close()` explicitly before `os.Remove()`.
4. Return `core.Artifact{ResourceID, Path, Size, CreatedAt}` - leave
   `Host`/`Plugin`/`Checksum` unset; the controller stamps those on after
   `Backup` returns (the plugin doesn't know BOP's inventory concepts).

### `Verify`

A cheap **structural** sanity check on the artifact `Backup` just
produced - not a full parse, not a restore. Runs immediately after
`Backup`, before checksum/upload. This is the first of BOP's three
verification tiers (see [Introduction](01-introduction.md)):

1. `plugin.Verify` - structural (this method).
2. `StorageProvider.Verify` - storage-level integrity (after upload).
3. Restore-test - actual recoverability (optional, see below).

Both built-in plugins check "non-empty" plus a cheap format marker:
`postgres.Verify` checks for pg_dump's own header comment; `filesystem.Verify`
checks for gzip's two-byte magic number. Neither does a full parse - that's
what tier 3 (an actual restore) is for.

### `Restore`

The one method with the most subtlety. `artifact.Path` is the source
dump/archive to restore **from**; `artifact.ResourceID` is the target
identifier to restore **into** (a database name, a directory path -
whatever your plugin's "resource" concept is).

`Restore` has two callers with very different stakes:

- **`bop restore`** (via `StorageProvider.Retrieve`, not this method) pulls
  a raw artifact down - it never calls plugin `Restore` at all. A real
  plugin-level restore into a live resource isn't wired into any CLI
  command today.
- **The controller's restore-test step**, when `verification.enabled` -
  see below.

#### Restore-test support

When the controller calls `Restore` as its optional verification step
(`verification.enabled` in config.yaml/inventory.yaml), it builds the
target artifact like this (`internal/controller/controller.go`,
`backupResource`):

```go
restoreTarget := artifact
restoreTarget.ResourceID = res.ID + plugin.RestoreTestSuffix // "-bop-verify"
```

`plugin.RestoreTestSuffix` (`internal/plugin/plugin.go`) is exported
specifically so a plugin can recognize this case and behave differently:
**a real restore must never have its own target deleted or redirected -
only a restore-test may do that**, since the target is guaranteed scratch
in that case only.

This matters more than it looks like it should, because of a real bug
found during infrastructure testing. The filesystem plugin originally used
`artifact.ResourceID` literally as its restore target - meaning a
restore-test for `/data/www` would try to create `/data/www-bop-verify` as
a **sibling** of the source directory. Against a real SSH target, this
failed: a backup user with read-only access to `/data` had no permission
to create anything there. The fix (`internal/plugin/filesystem/filesystem.go`):

```go
const restoreTestBase = "/tmp/bop-restore-test"

func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
    ...
    target := a.ResourceID
    isRestoreTest := strings.HasSuffix(a.ResourceID, plugin.RestoreTestSuffix)
    if isRestoreTest {
        target = path.Join(restoreTestBase, a.ResourceID)
    }
    // ... extract into target ...
    if isRestoreTest {
        // best-effort cleanup; a failed teardown must not fail an
        // otherwise-successful restore-test
    }
    return nil
}
```

Two lessons for a new plugin:

1. **Pick a restore-test destination your own SSH user can actually write
   to**, independent of whatever permissions it has on the source. `/tmp`
   is a reasonable default on Linux targets; don't assume write access
   next to the resource you're backing up.
2. **Clean up after a restore-test, best-effort.** A failed teardown must
   not fail the verification result (log and continue - mirror
   `controller.cleanupArtifact`'s treatment of its own temp-file cleanup as
   non-fatal). Otherwise `verification.enabled: true` left on nightly
   accumulates scratch data on every target host forever.

If your resource type can't safely support a restore-test at all (e.g.
`postgres`, which would need to provision a whole scratch database - not
yet built), **fail clearly rather than fake success**. Silently treating
an unrun restore-test as a pass is false confidence in exactly the
property ("can this actually be restored?") the feature exists to prove.

### `Health`

Reports whether the plugin can currently reach its target - cheap and
fast, not a full backup dry-run. Both built-in plugins just run `echo ok`
over the same SSH connection they'd use for a real backup. Not currently
called anywhere in the CLI or controller pipeline (no `bop health`
command exists yet) - implement it anyway, since it's part of the
interface contract and a future health-check command or API endpoint will
need it.

### `Metadata`

Identifies the plugin implementation and version - no I/O, no context.

```go
func (p *Plugin) Metadata() core.PluginMetadata {
    return core.PluginMetadata{Name: "filesystem", Version: "0.1.0"}
}
```

## Registration

A `PluginFactory` builds a `BackupPlugin` instance for a specific host:

```go
type PluginFactory func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error)
```

This is the seam a future out-of-process Plugin Engine would replace
(a factory that shells out to a subprocess instead of building an
in-process struct) without changing the interface plugin authors
implement. Both built-in plugins expose a `NewFactory(...)` that returns
one of these, closing over anything the factory itself needs (currently
just `config.yaml`'s `ssh.known_hosts_file`):

```go
func NewFactory(knownHostsFile string) func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
    return func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
        pgCfg, err := parseConfig(cfg)
        if err != nil {
            return nil, err
        }
        // ... validate srv.Host / srv.SSHUser / srv.SSHKey ...
        return &Plugin{...}, nil
    }
}
```

Wire it in `internal/cli/wiring.go`:

```go
ctl.RegisterPlugin("myplugin", myplugin.NewFactory(cfg.SSH.KnownHostsFile))
```

The registered name is what `inventory.yaml`'s `plugins` map keys against
(see [Inventory Reference](03-getting-started/inventory-reference.md)).

## Config Parsing

Each plugin's `config` block in `inventory.yaml` is a generic
`map[string]interface{}` (`inventory.PluginConfig.Config`) - parse your
own schema out of it. Both built-in plugins follow the same pattern
(`parseConfig(cfg *inventory.PluginConfig) (yourConfig, error)`):

- Fail clearly on missing required keys, don't default them silently.
- Never accept a plaintext password field - require a `*_env` key naming
  an environment variable instead (see `postgres`'s `password_env`), and
  read it via `os.Getenv` at parse time.
- A helper like `toStringSlice(v interface{}) ([]string, error)` for list
  fields is duplicated identically in both `postgres/config.go` and
  `filesystem/config.go` (small enough that sharing it wasn't worth the
  indirection) - copy that pattern rather than hand-rolling type
  assertions per field.

## SSH-Based Plugins

If your plugin connects over SSH (most will, given BOP's inventory model),
reuse the shared infrastructure rather than rolling your own:

- `internal/sshexec.Executor` / `SSHExecutor` - dials fresh per call,
  verifies every connection against `config.yaml`'s
  `ssh.known_hosts_file` (no insecure fallback - see
  [Configuration Reference](03-getting-started/configuration.md)).
- `internal/plugin/shellcmd.Build([]string{...})` - quotes an argv-style
  token list into a single shell command line. Build commands as a token
  slice, not a format string: mixing a wrapper command's own arguments
  (e.g. `docker exec`) with the command it runs is exactly the kind of
  nested-quoting bug this avoids.

Both are seam-tested via a small `Executor` interface (a `fakeExecutor`
test double records the command string and simulates stdin/stdout/errors)
so command construction is unit-testable without a real SSH server. See
`internal/sshexec/sshexec_test.go` for real in-process SSH server tests if
you need to verify actual protocol-level behavior (e.g. host-key
verification), not just command construction.

## Testing Expectations

Both built-in plugins were verified at three levels before being
considered done, and a new plugin should be too:

1. **Unit tests** against a fake `Executor`/similar seam - command
   construction, config parsing, error propagation, cleanup-on-failure.
2. **A local round-trip test of any non-trivial external tool
   interaction** where feasible (e.g. filesystem's tar/untar pair was
   tested against a real local `tar` binary before ever touching SSH).
3. **Real infrastructure**, even a throwaway one - both plugins were run
   against a real Docker container (SSH + real Postgres) at least once.
   This is what caught the restore-test permission bug described above; a
   local round-trip test alone did not, because it never exercised a
   differently-privileged remote user.

"Passes unit tests" and "verified against real infrastructure" are
different claims - state honestly which one applies when you're done.
