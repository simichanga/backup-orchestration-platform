# Roadmap

This is the honest list of what's built, what's designed but not built,
and what hasn't been scoped at all - written so the next unit of work
starts from a decision, not a guess. Like every other doc in this
project, it's written before the work rather than after, and it doesn't
prioritize on its own: the ordering below is a starting proposal, not a
commitment - see "How to use this list" at the bottom.

## Where things stand

**Shipped and verified against real infrastructure:** single-controller
orchestration (SSH-based postgres/filesystem plugins, restic storage,
SQLite metadata, cron scheduler, three-tier verification), an HTTP API
(read tokens + a separate write-scoped token for `POST /v1/backups`),
durable age-pruned events, and a browser UI served from the same binary.
CI builds/vets/formats/races on every push. See [README.md](../README.md)
for the full current-state summary.

## What's actually blocking production use

These are the gaps that matter if someone wants to run BOP for real, not
just try it out.

### 1. No multi-controller / HA story

**Milestone 1 of 2 shipped 2026-08-10.** One controller, one in-memory
queue. Losing the controller host loses in-flight scheduling state until
it's back (already-stored backups are unaffected - see
[docs/05-operations.md](05-operations.md#known-operational-behavior-read-before-youre-paged)).
[docs/06-high-availability.md](06-high-availability.md) is a complete
design proposal (Postgres advisory-lock leader election, active-passive,
a Postgres-backed shared queue table) - every open question it used to
list is decided (see that doc's "Decisions" section).

Implementation is two milestones, in order:

1. **Done: `metadata.driver: postgres`.** A single-controller deployment
   can now run against Postgres instead of SQLite - `internal/metadata`
   (jackc/pgx/v5, pure Go, keeps `CGO_ENABLED=0` cross-compilation working)
   and `internal/cli/wiring.go`. Verified against a real `postgres:16-
   alpine` container: the full existing metadata test suite passes against
   it (not a thinner Postgres-only smoke test), and a real `bop controller`
   run against it correctly applied schema, reconciled queued jobs on
   startup, and served reads/writes through the HTTP API. CI runs the same
   suite against a Postgres service container on every push. SQLite
   remains the default and is fully supported - this is opt-in.
2. **Not started: leader election + the shared queue table.** Builds on
   milestone 1's foundation. Still the highest-stakes remaining piece - it
   touches the crash-recovery model and introduces the first genuinely
   concurrent multi-process behavior in the codebase - treat it as its own
   scoped unit of work with its own real-infrastructure verification, not
   a continuation of milestone 1's commit.

### 2. Release & distribution

Done - [.github/workflows/release.yml](../.github/workflows/release.yml)
cross-compiles Windows and Linux amd64 binaries and publishes them to
GitHub Releases on a tag push.

### 3. Restore is CLI-only

`bop restore` deliberately never got an HTTP endpoint - the mutating API
endpoint work was scoped narrowly to `POST /v1/backups` only, since
restore is higher-stakes (it reads back and can overwrite real state) and
was judged to deserve its own scoping round rather than shipping by
default alongside trigger-backup. Still true; still needs that round.

### 4. Frontend test coverage

Done, in the sense that matters (every real logic path has a test) rather
than "100% of files touched." Vitest + React Testing Library are wired up
(`web/vite.config.ts`'s `test` block, `make test-web`, and a CI step), with
68 tests across 14 files covering: `src/api/client.ts`'s auth/401 handling
and `src/state/auth.tsx` (including a regression test for the exact
read-only-token-logs-out-the-session bug manual Playwright testing caught
once); the pure `src/lib/format.ts`/`src/lib/activity.ts`;
`src/components/Seal.tsx`'s `sealTierForEvents` (the verification-tier
derivation, not decoration); `src/hooks/useApi.ts` (stale-request guarding,
silent background-refresh-on-error, visibility-based poll pause/resume -
previously verified only by hand); `src/components/TriggerBackupModal.tsx`'s
write-scope-401 inline error path (the exact thing TESTING.md tells users to
go check manually); and all seven pages under `src/pages/` (status/host/job
filters, the type-then-Apply pattern on Events, the auto-select-first-host
effect on Snapshots, dashboard stat derivation with the recharts-dependent
`ActivityChart` mocked out since jsdom has no `ResizeObserver`). Every
regression test added this round that guards non-obvious behavior was
mutation-verified (break the behavior, confirm red, revert), not just
trusted on green. Pure layout/presentational pieces (`AppShell`, `Page`,
`StatusPill`) remain untested - deliberately, there's no real logic there
to regress.

### 5. Secrets-at-rest is minimal by design, not by accident

No BOP-side encryption or `.env` loading - every secret comes in via
`*_file`/`*_env`, delivered in production by systemd
`EnvironmentFile=`/`LoadCredential=`. This was a deliberate Phase 1 scope
decision, not an oversight, but it's worth re-examining once BOP is
actually handling production credentials somewhere that systemd's
delivery mechanism doesn't cover (e.g. Kubernetes, where the equivalent
is usually a mounted Secret - already compatible with `*_file`, so this
may turn out to need nothing new).

## Extensibility

### Out-of-process plugin loading

[docs/07-out-of-process-plugins.md](07-out-of-process-plugins.md) is a
complete design proposal (subprocess + gRPC, the HashiCorp go-plugin
pattern, minisign-based signing) with two open questions left (priority
relative to HA is now decided - HA is next, see above):
- minisign vs. cosign/GPG for signing.
- Whether a bad signature should block `bop controller` startup entirely
  or just skip that one plugin.

Also needs a fresh scoping round before any code - same standing policy
as HA.

## Frontend evolution

The UI (`web/`) covers everything the API exposes today. Two enhancements
scoped earlier (2026-08-10) both shipped same-day, before this doc's test-
coverage item was picked up: live client-side polling so a queued→running→
succeeded transition shows up without a manual reload, and a real activity
visualization on the dashboard built from actual job/event timestamps (not
sample data). See `2b6ea2b` for both.

## Known cosmetic nit

Fixed (2026-08-10). `bop health --plugin X` where `X` is registered but not
configured for that specific host used to return a raw config-parse error
("postgres: no config provided", doubly wrapped) instead of a clean
"not configured for this host" message - `Controller.BuildPlugin` (and
`runPipeline`, the same bug on the real backup-job path, not just the CLI
check) now checks whether the host's inventory entry lists the plugin at
all before ever reaching the plugin's own config parsing.

## Explicitly not planned

Carried forward from [docs/02-architecture.md](02-architecture.md) and
[docs/07-out-of-process-plugins.md](07-out-of-process-plugins.md) - these
were considered and rejected, not simply not-yet-done: gRPC alongside
REST (deferred until something actually needs it), a plugin
registry/marketplace, hot-reloading plugins without a controller restart,
OS-level sandboxing for out-of-process plugins beyond ordinary process
isolation, and non-Go plugin SDKs.

## How to use this list

Nothing here is prioritized by default - "prod-grade" and
"future-proof" cut in different directions depending on what BOP is
actually about to be used for (a single team's internal tool has a very
different HA/security bar than something exposed to other teams). Before
starting on any numbered item above, it should get the same treatment
everything else in this project has: a scoping conversation, not an
assumption.
