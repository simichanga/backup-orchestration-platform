# Configuration Reference

BOP uses a YAML configuration file (default: `/etc/bop/config.yaml`). All
settings can also be overridden by environment variables (prefixed `BOP_`) or
CLI flags.

Precedence, lowest to highest: config file < environment variables < CLI flags.

## Main Configuration

```yaml
# Path to the inventory file
inventory: /etc/bop/inventory.yaml

# Storage provider configuration
storage:
  provider: restic          # Currently only "restic"
  restic:
    repository: /mnt/backups/prod
    # Exactly one of password_file / password_env is required - see
    # "Secrets Management" below for which to pick.
    password_file: /etc/bop/restic-password.txt
    # password_env: RESTIC_REPO_PASSWORD
    extra_args: []           # Additional restic arguments (e.g., --verbose)
    concurrency: 2

# Controller behaviour
controller:
  concurrency: 4             # Max simultaneous backups
  job_timeout: 2h            # Max duration per backup job
  temp_dir: /tmp/bop

# Scheduler configuration
scheduler:
  cron_location: "Local"     # Timezone for cron expressions

# SSH host key verification for the postgres and filesystem plugins.
# Every connection is checked against known_hosts_file - there is no
# insecure/skip-verification option. Populate it the same way you would
# for the ssh CLI, e.g.:
#   ssh-keyscan -H your-host >> /etc/bop/known_hosts
ssh:
  known_hosts_file: /etc/bop/known_hosts

# Metadata database
metadata:
  driver: sqlite             # "sqlite" or "postgres"
  dsn: /var/lib/bop/metadata.db

# Optional read-only HTTP API (off by default - the CLI covers Phase 1's
# actual needs). v1 is REST-only, no mutating endpoints (no trigger/restore
# yet) - see docs/02-architecture.md#api. Every request needs a bearer
# token; exactly one of tokens_file/token_env is required when enabled.
api:
  enabled: false
  addr: ":9091"
  tokens_file: /etc/bop/api-tokens.txt
  # token_env: BOP_API_TOKEN

# Observability
metrics:
  port: 9090
  path: /metrics
logging:
  level: info                # debug, info, warn, error
  format: json               # json or text

# Verification defaults (global default; can be overridden per host in inventory.yaml)
verification:
  enabled: false
  # target_dir is currently not read by either plugin's restore-test path
  # (each plugin picks its own scratch location internally) - kept here
  # since it's part of the documented schema, not because it has an effect
  # yet. See docs/04-writing-plugins.md#restore-test-support.
  target_dir: /tmp/bop-verify

# Plugin registry (unused in Phase 1 - core plugins ship compiled into the
# bop binary; this is for future third-party/out-of-tree plugins)
plugins:
  dir: /usr/local/lib/bop/plugins   # Directory with plugin binaries
  allow_unsigned: false             # Require signed plugins

# Secrets management (future: age/SOPS integration)
secrets:
  env_file: /etc/bop/secrets.env
```

## Environment Variable Overrides

Any config key can be set as an environment variable using the BOP_ prefix
and underscores for nesting. For example:

```bash
BOP_STORAGE_RESTIC_REPOSITORY=/mnt/backups/other
BOP_CONTROLLER_CONCURRENCY=6
```

## CLI Flags

Flags take highest precedence over the config file and environment
variables. Today that's just one flag, present on every subcommand:

```bash
bop controller --config /etc/bop/config.yaml
```

There is no `--log-level` (or other per-key) flag yet - set
`logging.level` in `config.yaml` or via `BOP_LOGGING_LEVEL` instead. Run
`bop controller --help` for the current flag list.

## Inventory File Structure

See the [Inventory Reference](inventory-reference.md) for the full
`inventory.yaml` schema.

Per-host verification overrides the global `verification` block above:

```yaml
servers:
  prod-db:
    ...
    verification:
      enabled: true
      target_dir: /tmp/bop-restore-test
```

## Secrets Management

Never store a plaintext secret value in `inventory.yaml` or `config.yaml`.
Every secret BOP touches - the postgres plugin's database password, the
restic repository password, the API's bearer tokens - is delivered one of
two ways, your choice per secret:

- **`*_file`**: a path to a file containing just the secret
  (`storage.restic.password_file`, `api.tokens_file`). BOP reads the file's
  contents at the moment it's needed. `api.tokens_file` is the one
  exception to "just the secret": it supports one token per line (blank
  lines and `#` comments ignored), since an API can reasonably have more
  than one valid caller.
- **`*_env`**: the *name* of an environment variable holding the secret
  (`storage.restic.password_env`, postgres's `config.password_env`,
  `api.token_env`). BOP reads that variable from its own process
  environment at config/inventory load time (or, for `api.token_env`,
  controller startup) - which means **something has to put it there before
  BOP starts**. BOP does not read a `.env` file or any secrets store itself.

```yaml
postgres:
  config:
    password_env: PG_BACKUP_PASSWORD
```

```yaml
storage:
  restic:
    password_env: RESTIC_REPO_PASSWORD
    # or: password_file: /etc/bop/restic-password.txt
```

```yaml
api:
  enabled: true
  tokens_file: /etc/bop/api-tokens.txt
  # or: token_env: BOP_API_TOKEN (exactly one token, not a list)
```

### Delivering the value: systemd (recommended)

Since BOP doesn't load secrets itself, use the process supervisor to inject
them. On a systemd-managed host, two directives cover both mechanisms above:

- **`EnvironmentFile=`** for `*_env` secrets - a `KEY=value` file systemd
  reads and injects into BOP's environment before exec'ing it:
  ```ini
  # /etc/systemd/system/bop-controller.service
  [Service]
  EnvironmentFile=/etc/bop/secrets.env
  ExecStart=/usr/local/bin/bop controller
  ```
  ```bash
  # /etc/bop/secrets.env
  PG_BACKUP_PASSWORD=...
  RESTIC_REPO_PASSWORD=...
  ```
  Restrict this file's permissions the same way you would
  `restic-password.txt` below (`chmod 600`, owned by the service user).

- **`LoadCredential=`** for `*_file` secrets - systemd mounts the file into
  a runtime-only tmpfs directory that doesn't persist across reboots and
  isn't readable by other users, which is a real improvement over a
  permanent plaintext file on disk:
  ```ini
  LoadCredential=restic-password:/etc/bop/restic-password.txt
  ```
  and point `storage.restic.password_file` at
  `${CREDENTIALS_DIRECTORY}/restic-password` (systemd expands this into
  BOP's environment as `$CREDENTIALS_DIRECTORY`).

Without a process supervisor doing this, the simplest correct option is
still a `*_file` pointing at a permission-locked-down plaintext file - the
same trust model as an SSH private key file:

```bash
echo "my_restic_repo_pass" > /etc/bop/restic-password.txt
chmod 600 /etc/bop/restic-password.txt
```

**`secrets.env_file`** is a config key documenting the *shape* operators
should give this file - it is not currently read by BOP itself (there is no
built-in `.env` loader). Use `EnvironmentFile=` above, or your own
supervisor's equivalent, to actually get it into BOP's environment.

**(Future)** SOPS/age integration to encrypt secret values directly inside a
still-committable `inventory.yaml`/`config.yaml` is a real option if a
later phase's deployment model (e.g. a fleet with inventory checked into
git) needs it - deliberately not built for Phase 1's single-controller,
local-file deployment, where it would add a key-bootstrapping problem
without solving one that exists yet.

## Next

> **Note:** a dedicated User Guide doesn't exist yet. Continue to the
> [Quickstart](quickstart.md) to fully define your infrastructure.

