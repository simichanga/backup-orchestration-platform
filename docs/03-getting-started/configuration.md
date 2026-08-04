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
    password_file: /etc/bop/restic-password.txt
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

# API server (gRPC + REST)
api:
  grpc_addr: ":9091"
  rest_addr: ":9092"

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

Flags take highest precedence. Example:

```bash
bop controller --config custom.yaml --log-level debug
```

Run bop controller --help for all flags.

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

Never store passwords in inventory. Instead:

- Use password_env to reference an environment variable.
- Store secrets in a .env file specified by secrets.env_file.
- (Future) Integrate with SOPS/age to encrypt secrets directly in the
inventory file.

Example:
```yaml
postgres:
  config:
    password_env: PG_BACKUP_PASSWORD
```

Set PG_BACKUP_PASSWORD in secrets.env or via systemd environment.

## Restic Repository Password

The Restic repository password must be stored separately. Provide its path in
`storage.restic.password_file`. The file should contain only the password.

```bash
echo "my_restic_repo_pass" > /etc/bop/restic-password.txt
chmod 600 /etc/bop/restic-password.txt
```

## Next

> **Note:** a dedicated User Guide doesn't exist yet. Continue to the
> [Quickstart](quickstart.md) to fully define your infrastructure.

