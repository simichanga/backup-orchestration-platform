# Quickstart

This guide walks you through backing up a PostgreSQL database on a remote host
and verifying the backup can be restored.

## 1. Create an Inventory

Create `inventory.yaml`:

```yaml
servers:
  prod-db:
    host: 192.168.1.100
    ssh_user: bop
    ssh_key: /home/bop/.ssh/id_ed25519
    plugins:
      postgres:
        config:
          username: backup_user
          password_env: PG_BACKUP_PASSWORD
          databases:
            - myapp
    retention:
      daily: 7
      weekly: 4
      monthly: 3
    schedule: "0 3 * * *"
```

## 2. Set Secrets

Do not put passwords in inventory. Use environment variables or a .env file:

```bash
echo "PG_BACKUP_PASSWORD=supersecret" > /etc/bop/secrets.env
```

Reference the password via password_env as shown above.

## 3. Initialize a Restic Repository

On the controller, choose a storage location (local path, S3 bucket, etc.).

Initialize it:
```bash
restic init --repo /mnt/backups/prod-db
```

Note the repository password; store it securely.

Store the password where BOP will look for it (referenced as
`password_file` below):

```bash
echo "<the repository password from restic init>" > /etc/bop/restic-password.txt
chmod 600 /etc/bop/restic-password.txt
```

## 4. Configure BOP

Create config.yaml:

```yaml
inventory: /etc/bop/inventory.yaml
storage:
  provider: restic
  restic:
    repository: /mnt/backups/prod-db
    password_file: /etc/bop/restic-password.txt
    extra_args: []
logging:
  level: info
metrics:
  port: 9090
```

## 5. Trust the Target Host's SSH Key

BOP verifies every SSH connection against a known_hosts file - there is no
insecure/skip-verification option, so this step is required, not optional.
Add the target host's key the same way you would for the `ssh` CLI:

```bash
ssh-keyscan -H 192.168.1.100 >> /etc/bop/known_hosts
```

Verify the fingerprint against the host through a channel you trust (e.g.
console access, your provisioning tool's output) before trusting it - this
is the same verification `ssh` itself expects on a first connection.

## 6. Run the Controller

```bash
bop controller --config config.yaml
```

The controller loads inventory, registers plugins, and waits for scheduled
jobs. For an immediate backup, trigger it manually:

```bash
bop backup --host prod-db --plugin postgres
```

You’ll see logs like:
```
INFO  Backup job queued       host=prod-db plugin=postgres
INFO  Plugin discovery        databases=[myapp]
INFO  Artifact created        size=45MB checksum=sha256:ab12...
INFO  Uploading to restic     snapshot=abc123
INFO  Verification passed
INFO  Retention applied
INFO  Backup completed        duration=34s
```

## 7. Verify the Snapshot

List snapshots:

```bash
bop snapshot list --host prod-db
```

Output:

```bash
ID        Host      Plugin     Time                 Size
abc123    prod-db   postgres   2026-08-04 03:00:05  45MB
```

## 8. Test a Restore

BOP can automatically verify recoverability by restoring each backup to a
scratch location as part of the pipeline. Add a per-host override under
`prod-db` in inventory.yaml (see [Configuration Reference](configuration.md)
for the global default):

```yaml
servers:
  prod-db:
    ...
    verification:
      enabled: true
```

Both built-in plugins support this fully. The **postgres** plugin
provisions its own scratch `<database>-bop-verify` database for the
duration of the test and drops it afterward - `CREATE DATABASE` failing
because that name is already taken (a real resource collides with it, or a
previous restore-test crashed before cleaning up) is treated as "not
created by this run" and refused rather than silently reused, so a
restore-test never restores into or drops a database it doesn't own. The
**filesystem** plugin restores into a disposable directory it creates
itself, the same idea. See
[Writing a Plugin](../04-writing-plugins.md#restore-test-support) for the
full contract a plugin needs to support this.

You can always manually restore a snapshot's raw artifact, independent of
the (optional) automatic verification step above:

```bash
bop restore --snapshot abc123 --target /tmp/restored-db
```

## 9. Next Steps

- Add more servers to inventory - see the [Inventory Reference](inventory-reference.md).
- Explore retention policies.
- Set up Prometheus metrics scraping.
- [Write a custom plugin](../04-writing-plugins.md).