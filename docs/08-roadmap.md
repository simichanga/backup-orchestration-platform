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

One controller, one SQLite file, one in-memory queue. Losing the
controller host loses in-flight scheduling state until it's back (already
-stored backups are unaffected - see
[docs/05-operations.md](05-operations.md#known-operational-behavior-read-before-youre-paged)).
[docs/06-high-availability.md](06-high-availability.md) is a complete
design proposal (Postgres advisory-lock leader election, active-passive)
with three open questions still unanswered:
- Priority relative to out-of-process plugin loading (below).
- Whether the SQLite→Postgres metadata migration should ship as its own
  step ahead of HA, or bundled with it.
- Whether a shared queue (NATS/Redis, or a Postgres table) is needed on
  day one of HA, or whether a single active writer can keep the
  in-memory queue for longer than expected.

This is the highest-stakes item on this list - it touches the metadata
store, the queue, and the crash-recovery model - and per standing
project policy needs a fresh scoping round before any code is written,
regardless of how much autonomy is granted going into that session.

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

### 4. Frontend has no automated test coverage

Started. Vitest + React Testing Library are wired up
(`web/vite.config.ts`'s `test` block, `make test-web`, and a CI step), with
coverage of `src/api/client.ts`'s auth/401 handling and `src/state/auth.tsx`
- including a regression test for the exact read-only-token-logs-out-the-
session bug manual Playwright testing caught once (see
`web/src/state/auth.test.tsx`). Everything else under `web/` (components,
pages) still has zero coverage - this closed the highest-risk gap, not the
whole gap.

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
pattern, minisign-based signing) with three open questions:
- minisign vs. cosign/GPG for signing.
- Whether a bad signature should block `bop controller` startup entirely
  or just skip that one plugin.
- Priority relative to HA (above).

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

`bop health --plugin X` where `X` is registered but not configured for
that specific host returns a raw config-parse error instead of a clean
"not configured for this host" message. Deliberately left alone so far -
low severity, correct behavior either way, just a worse error message
than it should be.

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
