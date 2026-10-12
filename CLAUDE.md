# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

BOP (Backup Orchestration Platform) orchestrates backups across a fleet of
hosts over SSH: scheduling, inventory, verification, retention. It does not
implement its own storage engine - backup data is stored via
[restic](https://restic.net/), which BOP shells out to. Single Go binary
(`bop`) with an embedded React/TypeScript web UI.

Current status: Phase 1+2 - a working vertical slice (postgres + filesystem
plugins, restic storage, SQLite/Postgres metadata, in-process cron
scheduler) plus an optional HTTP API and embedded web console. No
multi-controller/HA yet and no out-of-process plugin loading yet - both have
written-but-not-implemented design docs (`docs/06-high-availability.md`,
`docs/07-out-of-process-plugins.md`). Read `README.md`'s Status section
before assuming a capability exists.

## Commands

Run `make` or `make help` for the authoritative list. The important ones:

```
make setup         Check required tools are installed, scaffold config.yaml/inventory.yaml/data/ (run this first)
make build         Build bop (rebuilds the web UI first)
make build-web     Rebuild only the embedded web UI (npm ci && npm run build under web/)
make run           Build, then run against CONFIG= (default config.yaml)
make dev           Run backend + frontend dev server together, hot-reloading (needs a real config.yaml)
make demo          Zero-config real demo: throwaway Docker target + real controller + browser
make demo-clean    Remove the demo's Docker container and temp files
make test          go test ./...
make test-web      npm --prefix web run test  (Vitest)
make vet           go vet ./...
make fmt           List any files gofmt would reformat
```

- Single Go test: `go test ./internal/controller/... -run TestName -v`
- Single Vitest test: `npm --prefix web run test -- src/path/to/File.test.tsx`
- `go build -o bin/bop ./cmd/bop` works standalone without `make`/Node -
  `internal/webui/dist` (the built web UI) is committed, so the frontend is
  already baked in.
- **After changing anything under `web/`, run `make build-web` and commit
  the result.** CI fails if `internal/webui/dist` doesn't match a fresh
  build (`git diff --quiet -- internal/webui/dist`).
- On Windows, `make` uses `cmd.exe` as SHELL and dispatches to
  `scripts/*.ps1`; on Linux/WSL it dispatches to `scripts/*.sh` (bash, not
  dash - uses arrays/`wait -n`). Both are maintained in parallel by hand;
  keep them in sync when changing setup/dev/demo behavior.
- The race detector (`go test -race`) needs a C toolchain and only runs in
  CI (`.github/workflows/ci.yml`), not on a plain Windows dev machine.
- CI also runs `internal/metadata`'s full test suite a second time against
  a real Postgres service container (`BOP_TEST_POSTGRES_DSN` env var) to
  cover the `metadata.driver: postgres` path - see
  `internal/metadata/metadata_test.go`'s `openTestStore`.
- `TESTING.md` covers `make demo`/`scripts/try-it-out.*` for a from-scratch
  manual walkthrough with real Docker infrastructure.

## Architecture

Hexagonal / ports-and-adapters. Three ports, defined as Go interfaces, with
concrete adapters behind them:

- **`plugin.BackupPlugin`** (`internal/plugin/plugin.go`) - a data source.
  Methods: `Discover`, `Backup`, `Restore`, `Verify` (structural sanity
  check, not storage integrity), `Health`, `Metadata`. Implementations:
  `internal/plugin/postgres`, `internal/plugin/filesystem`,
  `internal/plugin/shellcmd`. Phase 1: compiled directly into the binary,
  registered in-process via `ctl.RegisterPlugin(...)` in
  `internal/cli/wiring.go`. The controller never contains source-specific
  logic - adding a plugin never touches controller code.
- **`storage.StorageProvider`** (`internal/storage/storage.go`) - a backup
  destination. Methods: `Store`, `Retrieve`, `Verify`, `Delete`,
  `Snapshots`, `ApplyRetention`. Only implementation is
  `storage.ResticProvider` (`internal/storage/restic.go`), which shells out
  to the `restic` binary.
- **`queue.Queue`** (`internal/queue/queue.go`) - scheduler-to-controller
  handoff. Only implementation is an in-memory buffered queue
  (`internal/queue/memory.go`). **Durability contract**: the metadata
  service, not the queue, is the system of record for scheduled work - a
  job is persisted as `queued` in metadata *before* being enqueued, and
  `bop controller` startup re-enqueues anything still `queued` from a prior
  crash (`app.reconcileQueuedJobs` in `internal/cli/wiring.go`). Never
  assume the in-memory queue alone reflects pending work.

Shared domain types (`Resource`, `Artifact`, `Job`, `Snapshot`, `Policy`,
etc.) live in `internal/core/core.go` specifically to avoid import cycles
between plugin/storage/queue - put new cross-cutting types there, not in
one of the port packages.

### Request/job flow

`internal/scheduler` (cron via `robfig/cron`, one job per plugin configured
on a server) -> `Queue` -> `internal/controller` (`Controller.RunJob`, a
single serial consumer - deliberately not a worker pool, since
`ApplyRetention`'s `restic forget --prune` takes an exclusive repository
lock) -> for each job: `Discover` -> `Backup` -> `plugin.Verify` (fail fast,
before checksum/encrypt/upload) -> checksum -> optional encrypt ->
`StorageProvider.Store` -> record metadata -> `StorageProvider.Verify` ->
optional restore-test (`plugin.Restore` into a scratch target, ID suffixed
with `plugin.RestoreTestSuffix` - never the live resource) -> apply
retention -> emit events + metrics. See `docs/02-architecture.md` for the
full numbered pipeline and the three-tier verification model (structural ->
storage integrity -> restore test).

### Other components

- **`internal/metadata`** - system of record for jobs/snapshots/events.
  Two drivers: `sqlite` (`metadata.Open`) and `postgres`
  (`metadata.OpenPostgres`), selected by `metadata.driver` in config and
  dispatched in `internal/cli/wiring.go#buildApp`. Postgres is the
  documented prerequisite for eventual multi-controller HA
  (`docs/06-high-availability.md`) but is fully supported standalone too.
  Any new metadata behavior needs equivalent test coverage against *both*
  drivers, mirroring how CI runs `internal/metadata`'s suite twice.
- **`internal/events`** - structured event bus. `events.Multi` fans out to
  every subscriber with zero extra instrumentation in the controller or
  plugins: `events.LogPublisher` (structured logs), `metrics.Publisher`
  (Prometheus, `internal/metrics`), `metadata.EventPublisher` (durable
  storage in the `events` table, age-pruned by
  `internal/cli`'s `runEventPruner` per `metadata.event_retention`).
  Adding a new event sink means adding a new `events.Publisher`, not
  touching the controller.
- **`internal/api`** - optional REST API (`api.enabled`, off by default).
  Bearer-token auth with *separate* read and write token scopes
  (`api.tokens_file`/`api.token_env` vs
  `api.write_tokens_file`/`api.write_token_env`) - write is a superset of
  read, unsetting the write scope means no write access exists at all, not
  "reads still work." Only `POST /v1/backups` mutates; everything else
  (hosts/jobs/snapshots/events) is read-only. Restore stays CLI-only
  (`bop restore`) - deliberately not exposed over the API yet.
- **`internal/cli`** - Cobra commands. Phase 1 has no long-running daemon
  for most commands to talk to, so each command except `controller` builds
  its own in-process `app` (`internal/cli/wiring.go#buildApp`) and runs to
  completion. `buildApp` is the single place all the ports get wired
  together - read it first when tracing how a config value reaches runtime
  behavior.
- **`internal/sshexec`** - shared SSH execution helper plugins use to reach
  target hosts.
- **`internal/inventory`** - loads `inventory.yaml` (hosts, plugin
  assignments, retention policies, schedules) - the source of truth for
  *what* gets backed up, separate from `config.yaml` (*how* BOP itself
  runs).
- **`internal/webui`** - `//go:embed`s `web/`'s built output (`dist/`,
  committed) into the binary and serves it at `/`, same-origin with the
  API. It has no knowledge of the API beyond mounting static assets -
  everything the UI can do, the API already allowed.

### Web UI (`web/`)

React + TypeScript + Vite, react-router, plain CSS Modules (no
Tailwind/component library - deliberate, so the design stays intentional
rather than templated). Talks only to BOP's own `/v1/*` REST API,
same-origin; the dev server proxies `/v1` to `127.0.0.1:9091`
(`vite.config.ts`) so `npm run dev` needs a real `bop controller
--api.enabled=true` running alongside it, not a mock. Auth token lives in
`sessionStorage` only (`src/state/auth.tsx`, `src/api/client.ts`). Fonts
are self-hosted via `@fontsource` (no CDN - this can run air-gapped).
`vite.config.ts` writes the production build straight to
`../internal/webui/dist`. See `web/README.md` for the "vault ledger"
design language if touching UI (the `Seal` component,
`src/components/Seal.tsx`, reflects real verification events, not
decoration).

### Config vs inventory

Two separate YAML files with distinct jobs, both loaded in
`buildApp`: `config.yaml` (BOP's own runtime settings - storage, metadata
driver, API, metrics, logging) vs `inventory.yaml` (hosts and what to back
up on them). `*.example.yaml` files are the committed templates;
`make setup` copies them to the real (gitignored) files.

## Design docs worth reading before touching these areas

- `docs/02-architecture.md` - the full pipeline, event system, scalability
  model. Read before changing controller/plugin/storage interaction.
- `docs/04-writing-plugins.md` - required contract and testing expectations
  for a new `BackupPlugin` (includes what "verified against real
  infrastructure, not just unit tests" means here).
- `docs/05-operations.md` - HTTP API/web UI wire details, known operational
  limitations (read before assuming HA or crash-mid-job semantics).
- `docs/06-high-availability.md`, `docs/07-out-of-process-plugins.md` -
  scoped, locked-in design proposals for gaps that are not yet implemented.
  Check these before proposing a different design for either.
