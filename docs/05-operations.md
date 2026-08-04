# Running BOP in Production

This page is for actually operating a `bop controller` process, not just
getting one running once. It assumes you've already worked through the
[Quickstart](03-getting-started/quickstart.md) and have a working
`config.yaml`/`inventory.yaml`.

## A Hardened systemd Unit

[Installation](03-getting-started/installation.md) shows a minimal unit to
get started. This is what a production one should actually look like, tying
together the secrets delivery mechanisms from
[Configuration Reference](03-getting-started/configuration.md#secrets-management),
`known_hosts` setup, and basic sandboxing:

```systemd
# /etc/systemd/system/bop-controller.service
[Unit]
Description=Backup Orchestration Platform - controller
After=network-online.target
Wants=network-online.target

[Service]
User=bop
Group=bop

# *_env secrets (postgres's password_env, restic's password_env if you use
# it instead of password_file) resolve against whatever's in this file.
EnvironmentFile=/etc/bop/secrets.env

# *_file secrets (e.g. storage.restic.password_file) can instead point at a
# systemd-managed, non-persistent credential - swap the config value for
# ${CREDENTIALS_DIRECTORY}/restic-password if you use this.
LoadCredential=restic-password:/etc/bop/restic-password.txt

ExecStart=/usr/local/bin/bop controller --config /etc/bop/config.yaml
Restart=on-failure
RestartSec=5s

# Sandboxing: BOP only needs to read its own config/inventory/known_hosts,
# write its SQLite metadata db and temp_dir, and reach the network (SSH to
# targets, the restic binary, its own metrics port).
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/bop /tmp/bop
ReadOnlyPaths=/etc/bop

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bop-controller
sudo systemctl status bop-controller
journalctl -u bop-controller -f
```

`ProtectSystem=strict` makes the whole filesystem read-only to the process
except what's explicitly listed in `ReadWritePaths` - match those paths to
your actual `controller.temp_dir` and `metadata.dsn` directory. If you
change either in `config.yaml`, update `ReadWritePaths` to match, or the
controller will fail to write its metadata database / temp artifacts and
every job will fail at the first write.

## Secrets, Concretely

See [Configuration Reference](03-getting-started/configuration.md#secrets-management)
for the full `*_file`/`*_env` picture. The one thing worth restating here:
**BOP never loads a secrets file itself** - `EnvironmentFile=`/
`LoadCredential=` above are what actually get a secret into the process.
There's no BOP-side `.env` parser to reason about or misconfigure.

## Monitoring

`bop controller` exposes Prometheus metrics on `metrics.port`/`metrics.path`
(default `:9090/metrics`): `bop_jobs_total{host,plugin,status}`,
`bop_job_duration_seconds{host,plugin}`, `bop_artifacts_created_total`,
`bop_retention_applied_total`, `bop_restore_verifications_completed_total`.
There is no built-in alerting - wire these into your existing Prometheus/
Alertmanager stack. A reasonable starting alert: no successful
`bop_jobs_total{status="succeeded"}` increment for a host within its
expected backup interval plus some slack.

Logs are structured (`logging.format: json` by default) and go to stdout -
`journalctl -u bop-controller` picks them up under the systemd unit above
with no extra configuration.

## Known Operational Behavior (Read Before You're Paged)

- **Crash recovery is "fail forward," not "resume."** A job still
  `in_progress` when the controller starts is marked `failed`, not
  retried automatically - it re-runs on its next scheduled tick. A job
  that was `queued` (persisted but never dequeued, e.g. the process died
  between `CreateJob` and the in-memory queue picking it up) *is*
  re-enqueued on startup. If you need a failed job to run again sooner than
  its next scheduled tick, that's a manual `bop backup` today - see
  [Quickstart](03-getting-started/quickstart.md).
- **One job runs at a time, regardless of `controller.concurrency`.** That
  key is documented and parsed but not yet enforced - Phase 1's controller
  is deliberately a single serial consumer, because `restic forget --prune`
  takes an exclusive repository lock and concurrent jobs would collide on
  it. Don't tune `concurrency` expecting parallel backups; it currently has
  no effect.
- **A killed restic process (e.g. `controller.job_timeout` firing mid-backup)
  leaves a stale lock file in the repository, but this is not a fresh-boot
  hazard.** Verified directly against a real repository: restic 0.19.1
  detects that the lock-holding PID is dead (same machine) and does not
  block the next operation - no manual `restic unlock` is needed after an
  ordinary timeout-kill. This changes if a future phase runs multiple
  controllers against one shared repository (a dead PID on a *different*
  machine can't be verified this way) - re-check this behavior before
  relying on it in that topology.
- **Both built-in plugins support `verification.enabled` fully.** Postgres
  provisions its own scratch `<database>-bop-verify` database for the
  duration of the test and drops it afterward; if that name is already
  taken (a real resource collides with it, or a previous restore-test
  crashed before cleaning up), the restore-test fails clearly instead of
  touching a database it didn't create - see
  [Writing a Plugin](04-writing-plugins.md#restore-test-support) for the
  full contract. If you see this failure repeatedly for the same resource,
  something is left over on the target from a crashed run; drop the
  `<database>-bop-verify` database by hand to clear it.
- **No multi-controller / HA story.** One controller process, one SQLite
  metadata database, one in-memory job queue. Losing the controller host
  loses in-flight (not yet-persisted) scheduling state until it comes back;
  already-stored backups in the restic repository are unaffected.

## Restoring in an Emergency

`bop restore` (see [Quickstart](03-getting-started/quickstart.md)) pulls a
raw artifact straight from the storage provider - it does not go through a
plugin's `Restore` method, and it does not require the controller to be
running. This is deliberate: the path you'd actually use under pressure
doesn't depend on the same process that might be the thing that's down.
