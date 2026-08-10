# BOP - Backup Orchestration Platform

[![CI](https://github.com/simichanga/backup-orchestration-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/simichanga/backup-orchestration-platform/actions/workflows/ci.yml)

BOP orchestrates backups across a fleet of hosts: scheduling, inventory,
verification, and retention. It doesn't implement its own storage engine -
backup data is stored via [restic](https://restic.net/), which BOP shells
out to.

## Status

Phase 1 (documented in [docs/01-introduction.md](docs/01-introduction.md))
is a complete, working vertical slice: a single controller binary,
SSH-based plugins for PostgreSQL and filesystem backups, Restic storage,
SQLite metadata, and an in-process cron scheduler. Every code path
(including both of postgres's invocation modes - direct and
`docker exec`) has been run against real infrastructure at least once, not
just unit-tested - see [docs/04-writing-plugins.md](docs/04-writing-plugins.md#testing-expectations)
for what that verification covered.

There's an optional HTTP API (`api.enabled`, off by default, bearer-token
auth) covering hosts/jobs/snapshots/events (read-only tokens) plus
triggering an ad-hoc backup (`POST /v1/backups`, a separate write-scoped
token), and a browser-based ops console (`web/`, a React/TypeScript SPA
embedded into the `bop` binary and served from the same port - no
separate deploy or Node.js runtime required to run it) - see
[docs/05-operations.md](docs/05-operations.md#http-api-and-web-ui).
Every controller event is durably persisted and age-pruned (`metadata.db`'s
`events` table, `metadata.event_retention`) - see
[docs/02-architecture.md](docs/02-architecture.md#event-system). There is
no multi-controller/HA story, restore still stays CLI-only, and there's no
out-of-process plugin loading yet - see
[docs/02-architecture.md](docs/02-architecture.md) and
[docs/05-operations.md](docs/05-operations.md#known-operational-behavior-read-before-youre-paged)
for the current honest limitations. Design proposals (not implemented)
exist for both of those gaps:
[docs/06-high-availability.md](docs/06-high-availability.md) and
[docs/07-out-of-process-plugins.md](docs/07-out-of-process-plugins.md).
See [docs/08-roadmap.md](docs/08-roadmap.md) for the full prioritized-but
-not-yet-decided list of what's next.

## Getting Started

New machine? `make setup` checks every tool this needs is actually
installed and working, and tells you exactly which one is missing if
not - then it scaffolds `config.yaml`/`inventory.yaml`/`data/` from the
committed `*.example.yaml` templates so `make run`/`make dev` have
something real to point at immediately.

Just want to see it work? [TESTING.md](TESTING.md) - `make demo` spins up
a real demo and opens the web UI, no manual config required.

1. [Installation](docs/03-getting-started/installation.md)
2. [Quickstart](docs/03-getting-started/quickstart.md) - configure a host,
   run a backup, list snapshots, test a restore.
3. [Configuration Reference](docs/03-getting-started/configuration.md)
4. [Inventory Reference](docs/03-getting-started/inventory-reference.md)
5. [Running BOP in Production](docs/05-operations.md) - systemd unit,
   secrets delivery, monitoring, operational gotchas.

Writing a new plugin (a new data source beyond postgres/filesystem)? Start
at [docs/04-writing-plugins.md](docs/04-writing-plugins.md).

## Building and running

`make` (bare, or `make help`) lists every target:

```
make setup         Check required tools are installed and working, scaffold config.yaml/inventory.yaml/data/
make build         Build bop (rebuilds the web UI first)
make build-web     Rebuild only the embedded web UI
make run           Build, then run the bundled binary against CONFIG= (default config.yaml)
make dev           Run the backend and the frontend dev server together, hot-reloading (needs CONFIG=)
make demo          Zero-config real demo: throwaway Docker target + real controller + browser
make demo-clean    Remove the demo's Docker container and temp files
make test          go test ./...
make vet           go vet ./...
make fmt           List any files gofmt would reformat
make clean         Remove bin/
```

Every target works the same way on Windows (PowerShell, `scripts/*.ps1`)
and Linux (bash, `scripts/*.sh`) - `make` picks the right script for the
OS it's running on. Verified on both: Windows natively, Linux via a real
WSL2 Ubuntu environment (not just Git Bash).

`make run`/`make dev` need a real `config.yaml` (`CONFIG=path` to override
the default) - `make demo` doesn't, it builds a real one for you. See
[TESTING.md](TESTING.md) for what `make demo`/`scripts/try-it-out.*`
actually do.

Plain `go build -o bin/bop ./cmd/bop` also works and doesn't need `make`
or Node.js at all - `internal/webui/dist` (the built web UI, see
`web/README.md`) is committed, so the frontend is already baked in. After
changing anything under `web/`, run `make build-web` and commit the
result - CI checks that `internal/webui/dist` isn't stale.

[.github/workflows/ci.yml](.github/workflows/ci.yml) runs the same build/vet/fmt
checks on every push, plus `go test -race ./...` - the race detector needs a C
toolchain, so it only runs in CI, not on a plain Windows dev machine.

## Architecture

Hexagonal / ports-and-adapters: `BackupPlugin` (a data source), `StorageProvider`
(a backup destination), and `Queue` (scheduler-to-controller handoff) are
the ports. See [docs/02-architecture.md](docs/02-architecture.md) for the
full design, including the three-tier verification model (structural ->
storage integrity -> restore test) and the job-durability contract that
makes the metadata service, not the queue, the system of record for
scheduled work.
