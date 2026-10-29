# Deploying BOP to hosts

How `bop` gets onto the machines that run it, and how it stays current.
Linux-only (unlike `scripts/`, there is no PowerShell twin).

## Model

```
git tag v1.2.0-rc.1 ──► release.yml (vet, test, build, GitHub prerelease)
                              │
             bop-update.timer polls every 15 min (pull, no inbound access)
                              ▼
  staging (BOP_CHANNEL=prerelease)  ── canary for every release candidate
git tag v1.2.0      ──► GitHub release (stable)
                              ▼
  prod    (BOP_CHANNEL=stable)      ── only ever runs non-prerelease tags
```

- **CI** (`.github/workflows/ci.yml`) runs on every push/PR.
- **Release** (`.github/workflows/release.yml`) fires on `v*.*.*` tags.
  A tag containing `-` (`v1.2.0-rc.1`) is published as a prerelease.
- **Delivery** is pull-based: `bop-update.sh`, run by a `systemd --user`
  timer on each host, installs the newest release on that host's channel.
  It verifies the tarball's sha256 against the release's `checksums.txt`,
  keeps the last 3 releases under `~/bop/releases/`, swaps the `current`
  symlink atomically, restarts `bop.service`, and rolls back if the new
  version doesn't come up. It skips (and retries next tick) while a
  backup job is in progress or queued.

  Pull rather than a self-hosted Actions runner: the hosts are on a
  private network, and a self-hosted runner on a public repository runs
  workflow code from fork PRs on that machine.

Release flow: tag `vX.Y.Z-rc.N`, wait for staging to pick it up and run a
backup (`journalctl --user -u bop -f` on staging), then tag `vX.Y.Z` on
the same commit to ship it to prod.

## Layout on a host

```
~/bop/
  current -> releases/vX.Y.Z      what bop.service runs
  releases/                       last 3 releases
  bin/bop-update                  the updater
  config.yaml, inventory.yaml     host-specific (not in git)
  restic-password                 repository password (600) - keep a copy off-host!
  update.env                      BOP_CHANNEL=stable|prerelease
  data/                           metadata.db, known_hosts, vault_known_hosts,
                                  api-read-token, tmp/
~/.local/bin/restic
~/.config/systemd/user/{bop.service,bop-update.service,bop-update.timer}
```

## Backup vault layout

One restic repository per source machine, under the vault account's home:

```
~/bop/<machine>/        restic repo (encrypted, deduplicated)
   └ snapshots grouped by host = app (inventory server name)
                         tag resource:<database>
                         time = date+time of the backup
```

Browse it with restic from any machine holding that repo's password:

```
restic -r sftp:<vault>:/home/<vault-user>/bop/simi-prod snapshots --host presa
restic -r ... ls latest --host presa
```

Each host's `bop_vault` key is authorized on the vault as
`restrict,command="/usr/lib/openssh/sftp-server"`: SFTP only, no shell or
forwarding. That is not a chroot - a compromised host could still delete
another host's repository. Real ransomware resistance needs an
append-only target (e.g. `rest-server --append-only`).

## Adding a host

1. Copy `deploy/` to the host and run `bootstrap.sh` there (see its header
   for the variables). Pin the vault's host key from a trusted
   `known_hosts`, not a fresh scan.
2. Authorize the printed `bop_vault` key on the vault account.
3. Write `~/bop/config.yaml` (template: `config.example.yaml`) and
   `~/bop/inventory.yaml`. Servers are SSH'd as `127.0.0.1` with
   `ssh_key: ~/.ssh/bop_local` (absolute path).
4. `restic init` the repository:
   `RESTIC_PASSWORD_FILE=~/bop/restic-password ~/.local/bin/restic -r <repo> init`
5. Copy `~/bop/restic-password` into your password manager. Without it the
   repository is unreadable.
6. The timer installs the newest release on its channel within 15 minutes,
   or run `systemctl --user start bop-update` to do it now.

## A second destination (e.g. logs on their own server)

A server in `inventory.yaml` can store into a different repository than
the default with `storage: <name>`, defined in `config.yaml` under
`storage.repositories.<name>`. To set one up on a host:

1. Run `bootstrap.sh` again with `VAULT_ALIAS=bop-<name>` and that
   server's `VAULT_HOST`/`VAULT_USER`/`VAULT_KNOWN_HOSTS`. It adds a
   separate key (`~/.ssh/bop_<name>`), alias, pinned host key and
   repository password (`~/bop/restic-password-<name>`).
2. Authorize the printed key on that server (SFTP-only, as above).
3. Add the repository to `config.yaml`, `restic init` it, and keep a copy
   of its password off-host.
