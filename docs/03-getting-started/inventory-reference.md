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
| `password_env` | string        | yes      | Name of an environment variable, already present in the `bop` process's own environment (e.g. via systemd `EnvironmentFile=` - see [Running BOP in Production](../05-operations.md)), holding the password. Never put the password in inventory.yaml directly. |
| `databases`    | list of string| yes, at least one | Database names to back up. Each becomes one resource (one job step, one snapshot). |
| `container`    | string        | no       | Docker container name. When set, `pg_dump`/`psql` run via `docker exec [-i] <container> ...` instead of directly on the host. |

Each configured database is dumped with `pg_dump`'s default plain-text SQL
format (not the custom/directory format), so restore only ever needs
`psql`, never `pg_restore`.

The restore-test pipeline step (`verification.enabled`) is fully
functional for this plugin: it provisions a scratch
`<database>-bop-verify` database, restores into it, and drops it
afterward - see
[Writing a Plugin](../04-writing-plugins.md#restore-test-support) for the
ownership-safety contract this relies on.

### `mariadb`

The MariaDB counterpart of `postgres`: connects over SSH and runs
`mariadb-dump`/`mariadb` on the host, or inside a container via
`docker exec` when `container` is set. Needs the `mariadb-*` client
binaries (MariaDB 10.4.6+; the official images ship them).

| Key                      | Type          | Required | Description |
|--------------------------|---------------|----------|--------------|
| `username`               | string        | yes      | MariaDB user to connect as. |
| `password_env`           | string        | one of these two | Name of an environment variable in the `bop` process's environment holding the password - same convention as `postgres`. |
| `password_container_env` | string        | one of these two | Name of an environment variable *inside* `container` holding the password, e.g. the official image's `MARIADB_ROOT_PASSWORD` (or legacy `MYSQL_ROOT_PASSWORD`). The password is expanded inside the container straight into `MYSQL_PWD`, so it never reaches the controller or any command line. Requires `container`. |
| `databases`              | list of string| yes, at least one | Database names to back up, one resource each. |
| `container`              | string        | no       | Docker container name. |

Dumps use `--single-transaction` (a consistent, non-locking snapshot for
InnoDB tables) plus `--routines --triggers --events`. The database is not
named via `--databases`, so the dump carries no `USE` statement and can
be restored into any database name. The structural `Verify` also checks
for `mariadb-dump`'s closing `-- Dump completed` line, catching a
truncated dump.

The plugin config is per server, so two MariaDB containers on one host
are two inventory servers sharing the same `host` (e.g. `prod-app` and
`prod-billing`). Retention is scoped by server name, so they don't prune
each other. Restore-test works as for `postgres` (scratch
`<database>-bop-verify` database, created and dropped on the target
server).

### `logs`

Backs up host logs over SSH: the systemd journal (where rsyslog's
`/var/log/syslog` comes from) and Docker container logs, as plain text.

| Key                  | Type           | Required | Description |
|----------------------|----------------|----------|--------------|
| `journal`            | bool           | one of these two | Back up the journal (`journalctl -o short-iso-precise`). The SSH user needs `systemd-journal` (or `adm`) group membership. |
| `containers`         | list of string | one of these two | Container name patterns (`path.Match` syntax, `"*"` = all, running or stopped). Matched against `docker ps -a` on every run, so new containers are picked up automatically. The SSH user needs docker access. |
| `exclude_containers` | list of string | no       | Patterns removed from `containers`' matches. |
| `window`             | duration       | no, default `72h` | How far back each run reaches. |

Resources are `journal` and `container-<name>`, one snapshot each per run.
Each run captures a rolling window rather than tracking a cursor:
stateless, and gap-free as long as fewer consecutive runs are missed than
the window spans. Consecutive snapshots overlap, which restic
deduplicates, so a wider window costs dump time, not repository space.

Logs are not restorable in place: `Restore` always fails, so never set
`verification:` on a server using this plugin. Read a backed-up log with
`bop restore --snapshot <id> --target <file>`.

Retention caveat: `forget --keep-daily N` keeps the last N days *that
have snapshots*, per resource. A removed or renamed container's snapshots
stop being replaced and are kept indefinitely; `forget` them by hand
(`restic forget --tag resource:container-<name> ...`) when no longer
wanted.

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

The restore-test pipeline step is functional for this plugin too: it
restores into a disposable directory under `/tmp` (regardless of what
`verification.target_dir` is set to - see
[Configuration Reference](configuration.md)) and cleans up afterward.

### `informix`

Backs up IBM Informix databases through `ifx-agent`, a separate gRPC
subprocess from the [informix-odbc](https://github.com/simichanga/informix-odbc)
project that `bop` spawns locally and talks to - not SSH. The `host`/
`ssh_user`/`ssh_key` fields above are not used by this plugin at all;
the Informix server address lives in its own `host`/`port` keys below.

| Key            | Type           | Required | Description |
|----------------|----------------|----------|--------------|
| `host`         | string         | yes      | Informix server hostname or IP. |
| `port`         | number         | yes      | Informix server port (e.g. `9088`). |
| `username`     | string         | yes      | Informix user to connect as. |
| `password_env` | string         | yes      | Name of an environment variable holding the password - same convention as `postgres`'s `password_env` above. |
| `server_name`  | string         | yes      | The `INFORMIXSERVER` value (Informix's own server identifier, not the hostname). |
| `database`     | string         | yes      | Database name. |
| `tables`       | list of string | yes, at least one | Table names to back up. Each becomes one resource (one job step, one snapshot). |
| `agent_path`   | string         | yes      | Path to the `ifx-agent` binary on whatever host runs `bop controller`. |

```yaml
plugins:
  informix:
    config:
      host: 10.0.0.5
      port: 9088
      username: informix
      password_env: IFX_BACKUP_PASSWORD
      server_name: informix
      database: appdb
      tables:
        - customers
        - orders
      agent_path: /usr/local/bin/ifx-agent
```

This config can also be set (or changed) without touching
`inventory.yaml` at all, through the API or web UI - see
[Web-configurable plugin settings](../05-operations.md#web-configurable-plugin-settings).

Restore re-inserts rows into the table named in the backup artifact -
it does not recreate a dropped table from scratch (that's planned, not
built yet). The restore-test pipeline step runs the same insert path
against the real table, inside a transaction that is always rolled
back, never committed - there is no scratch table, since Informix in
this setup does not support `CREATE TABLE ... (LIKE ...)` as a
structure copy.

### `docker`

Mentioned as an example of a no-config plugin (`docker:` with a null
value) in [Introduction](../01-introduction.md), but **not implemented
yet** - only `postgres`, `filesystem`, and `informix` are real,
registered plugins in Phase 1. An inventory entry naming `docker` will
fail at job-run time with "no plugin registered for docker".

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
