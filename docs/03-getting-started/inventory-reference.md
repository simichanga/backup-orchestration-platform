# Inventory Reference

`inventory.yaml` is the source of truth for hosts, plugin assignments,
retention policies, and schedules (see
[Configuration Reference](configuration.md) for `config.yaml`, which is a
separate file). This page documents the full schema; see the
[Quickstart](quickstart.md) for a worked end-to-end example.

## Top-Level Structure

```yaml
servers:
  <server-name>:
    host: ...
    ssh_user: ...
    ssh_key: ...
    plugins:
      ...
    retention:
      ...
    schedule: "..."
    verification:
      ...
```

`<server-name>` is an arbitrary logical identifier you choose (e.g.
`prod-db`, `web-01`) - it's what you pass to `--host` on `bop backup` and
`bop snapshot list`, and what shows up as the `host` label on events,
snapshots, and Prometheus metrics. It does not need to match the actual
hostname.

## Server Fields

| Field          | Type   | Required                          | Description |
|----------------|--------|------------------------------------|--------------|
| `host`         | string | yes                                | Hostname or IP address the postgres/filesystem plugins SSH into. Connected to on port 22 (not yet configurable - see [Configuration Reference](configuration.md)'s deferred-items note). |
| `ssh_user`     | string | yes, if any plugin needs SSH       | SSH username. |
| `ssh_key`      | string | yes, if any plugin needs SSH       | Path to a private key file (any format `golang.org/x/crypto/ssh.ParsePrivateKey` accepts - ed25519, RSA, ECDSA). No passphrase-protected keys yet. |
| `plugins`      | map    | yes, at least one                  | See [Plugins](#plugins) below. |
| `retention`    | object | yes                                | Daily/weekly/monthly/yearly counts - see [Retention](#retention). |
| `schedule`     | string | no                                 | A standard 5-field cron expression (e.g. `"0 3 * * *"`). Omit to leave this server manual-only, triggered with `bop backup`. Validated at `bop controller` startup - an invalid expression is a startup error, not a silently-never-firing schedule. |
| `verification` | object | no                                 | Per-host override of `config.yaml`'s global `verification` block - see [Verification Override](#verification-override). |

The `host` connects to whatever the SSH server's own host key verification
demands - see `config.yaml`'s `ssh.known_hosts_file`
([Configuration Reference](configuration.md)). Populate it with
`ssh-keyscan` before a server's first real connection.

## Plugins

`plugins` is a **map, not a list**: a plugin with no configuration has a
null value, and a plugin that needs configuration nests it under a
`config` key. This isn't cosmetic - Viper (used for `config.yaml`) silently
drops null-valued YAML keys, which is exactly why `inventory.yaml` is
parsed directly with `yaml.v3` instead (see `internal/inventory`'s package
doc if you're touching that code).

```yaml
plugins:
  postgres:
    config:
      username: backup_user
      password_env: PG_BACKUP_PASSWORD
      databases:
        - myapp
  filesystem:
    config:
      paths:
        - /var/www
```

A server with multiple plugins gets one job per plugin per scheduled tick
(and one snapshot per resource, per plugin) - not one combined job.

### `postgres`

Connects over SSH and runs `pg_dump`/`psql` directly on the target host,
or inside a Docker container via `docker exec` when `container` is set -
most Postgres deployments run containerized, hence the option.

| Key            | Type          | Required | Description |
|----------------|---------------|----------|--------------|
| `username`     | string        | yes      | Postgres role to connect as. |
| `password_env` | string        | yes      | Name of an environment variable (in the `bop` process's environment, e.g. via `secrets.env_file`) holding the password. Never put the password in inventory.yaml directly. |
| `databases`    | list of string| yes, at least one | Database names to back up. Each becomes one resource (one job step, one snapshot). |
| `container`    | string        | no       | Docker container name. When set, `pg_dump`/`psql` run via `docker exec [-i] <container> ...` instead of directly on the host. |

Each configured database is dumped with `pg_dump`'s default plain-text SQL
format (not the custom/directory format), so restore only ever needs
`psql`, never `pg_restore`.

**Known limitation:** the restore-test pipeline step
(`verification.enabled`) is not yet functional for this plugin - see
[Writing a Plugin](../04-writing-plugins.md#restore-test-support).

### `filesystem`

Connects over SSH and streams a gzip-compressed tar archive of each
configured path.

| Key         | Type           | Required | Description |
|-------------|----------------|----------|--------------|
| `paths`     | list of string | yes, at least one | Absolute paths on the target host. Each becomes one resource. Must be directories (see below). |
| `excludes`  | list of string | no       | `tar --exclude` patterns, applied to every path. |

Paths are assumed to be directories: the plugin `tar`s the directory's
contents (`-C <parent> <basename>`) and, on restore, extracts with
`--strip-components=1` into the target - a bare file path will not restore
correctly.

Unlike `postgres`, the restore-test pipeline step is functional for this
plugin: it restores into a disposable directory under `/tmp` (regardless of
what `verification.target_dir` is set to - see
[Configuration Reference](configuration.md)) and cleans up afterward.

### `docker`

Mentioned as an example of a no-config plugin (`docker:` with a null
value) in [Introduction](../01-introduction.md), but **not implemented
yet** - only `postgres` and `filesystem` are real, registered plugins in
Phase 1. An inventory entry naming `docker` will fail at job-run time with
"no plugin registered for docker".

## Retention

```yaml
retention:
  daily: 7
  weekly: 4
  monthly: 12
  yearly: 0
```

All four fields are non-negative integers, applied once per job (not per
resource) via the storage provider's retention policy - for Restic, this
maps to `restic forget --keep-daily/--keep-weekly/--keep-monthly/--keep-yearly`,
scoped to the job's host via `--host`/`--group-by host,tags` so retention
never spans unrelated hosts or resources. A policy with every field at `0`
is refused before it would run an unscoped `forget` against the whole
repository.

## Verification Override

```yaml
servers:
  prod-db:
    ...
    verification:
      enabled: true
      target_dir: /tmp/bop-restore-test
```

A per-host `verification` block **replaces** `config.yaml`'s global default
wholesale for that host - it is not merged field-by-field. This is
deliberate: a bare `enabled: true` with no other fields can't be
distinguished from "explicitly false" within a partial merge, so an
override is all-or-nothing. Omit the block entirely to inherit the global
default unchanged.

See [Writing a Plugin](../04-writing-plugins.md#restore-test-support) for
which plugins currently support this, and `target_dir`'s current status
(not read by either built-in plugin - see
[Configuration Reference](configuration.md)).

## Full Example

```yaml
servers:
  prod-db:
    host: 192.168.1.10
    ssh_user: bop
    ssh_key: /home/bop/.ssh/id_ed25519
    plugins:
      postgres:
        config:
          username: backup_user
          password_env: PG_BACKUP_PASSWORD
          databases:
            - myapp
            - analytics
      filesystem:
        config:
          paths:
            - /var/www
          excludes:
            - "*.log"
    retention:
      daily: 7
      weekly: 4
      monthly: 12
    schedule: "0 3 * * *"
    verification:
      enabled: true

  web-01:
    host: 192.168.1.20
    ssh_user: bop
    ssh_key: /home/bop/.ssh/id_ed25519
    plugins:
      filesystem:
        config:
          paths:
            - /var/www/html
    retention:
      daily: 7
    # No schedule: manual-only, triggered with "bop backup --host web-01 --plugin filesystem".
```
