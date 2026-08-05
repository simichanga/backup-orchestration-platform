# Multi-Controller / High Availability (Proposal)

**Status: proposal, not implemented.** This is a design doc, not a spec of
shipped behavior - written the same way `docs/01-introduction.md` and
`docs/02-architecture.md` preceded Phase 1's code, per this project's
docs-first practice. Nothing in this file should be treated as true of the
current codebase. It exists to have something concrete to react to before
any of it gets built; see the open questions at the end.

## The problem, precisely

Today (per `docs/05-operations.md`'s "Known Operational Behavior"): one
controller process, one local SQLite metadata database, one in-memory job
queue. Losing the controller host loses:

- All in-flight scheduling state (jobs persisted as `queued` but not yet
  dequeued survive, per the durability contract - but nothing is currently
  running the scheduler or consumer to act on them).
- The ability to trigger, monitor, or read anything (CLI and the optional
  HTTP API both need the local SQLite file and the in-process controller).

Already-stored backups in the restic repository are unaffected - this is
purely a control-plane availability problem, not a data-loss one. That
framing matters: HA here means "keep scheduling and serving requests
despite losing a host," not "protect backup data," which restic's own
repository already handles independently of BOP's controller uptime.

## Why active-passive, not active-active

BOP's whole Phase 1 controller design centers on one invariant: **only one
process may run `restic forget --prune` against a given repository at a
time**, because prune takes an exclusive repository lock (see
`docs/05-operations.md`'s "one job runs at a time" note). That's why
Phase 1 is a single serial consumer instead of a worker pool even within
one process.

An active-active multi-controller design (each controller owns a subset
of hosts, all running concurrently) would have to re-solve that exact
problem across process boundaries - via per-repository distributed locking,
which is real complexity, for a use case (higher backup throughput) BOP
hasn't shown it needs yet. Phase 1's docs already flag
`controller.concurrency` as unenforced for the same reason.

**Recommendation: active-passive.** Multiple controller processes run;
exactly one is the active leader (runs the scheduler and drains the
queue) at any time; the rest are hot standbys that take over automatically
if the leader dies. This solves the actual stated problem (a dead
controller host blocks everything) without touching the single-writer
invariant at all - there is still only ever one process running jobs,
just not always the same one.

## What has to become shared

Two things are currently local to one process and would need to become
shared state all controllers can see:

### Metadata store: SQLite → PostgreSQL

Already anticipated, not yet built: `config.MetadataConfig.Driver`
accepts `"postgres"` today and `buildApp` rejects it explicitly
(`metadata.driver %q is not yet implemented`) rather than pretending to
support it. `docs/02-architecture.md`'s Technology Stack table already
lists "SQLite → PostgreSQL." This proposal doesn't change that direction,
just makes it concrete: implementing `metadata.driver: postgres` is a
prerequisite for HA, not a separate later decision - every controller
process needs to see the same jobs/snapshots/events tables.

### Queue: in-memory → shared

Also anticipated: `internal/queue/queue.go`'s doc comment already says
"later phases can swap in a shared queue (NATS, Redis) for multi-controller
deployments without changing this interface or its callers." This proposal
doesn't relitigate that choice. One addition worth flagging: a shared
queue is necessary but not sufficient by itself - see leader election
below for why "multiple consumers pulling from one shared queue" is *not*
the model being proposed here, even though NATS/Redis could technically
support it.

## Leader election

This is the part existing docs don't address yet - what actually decides
which controller is active.

**Recommendation: PostgreSQL advisory locks**
(`pg_advisory_lock`/`pg_try_advisory_lock`), not a new dependency like
etcd or Consul. Reasoning: HA already requires Postgres for the metadata
store (previous section), so leader election riding on the same
connection is one fewer moving part, not one more - consistent with this
project's demonstrated preference throughout Phase 1/2 for reusing an
already-required dependency over adding a new one (e.g. the API's static
tokens instead of standing up an OIDC provider).

Mechanics:

- Every controller process, on startup, attempts
  `pg_try_advisory_lock(<fixed key>)` on its metadata connection in a
  retry loop (e.g. every 5s if it fails).
- Whichever process holds the lock is the leader: it, and only it, starts
  the scheduler and the queue consumer.
- PostgreSQL advisory locks are **session-scoped** - if the leader's
  connection dies (process crash, network partition, host loss), Postgres
  releases the lock automatically. A standby's next retry acquires it and
  takes over. No manual unlock step, no separate heartbeat/TTL mechanism
  to build - Postgres's own connection liveness *is* the heartbeat.
- Every controller process (leader or standby) can still serve `GET`
  requests on the HTTP API and `/metrics` from the shared Postgres-backed
  metadata store, regardless of leadership - only scheduling/dispatch/
  consumption is leader-exclusive. This gives useful read-path redundancy
  "for free," not just failover for writes. `POST /v1/backups` on a
  standby should be rejected (503, "not the leader, retry against the
  active controller") or transparently proxied to the current leader - an
  open question below.

Failover time is bounded by two knobs: how often standbys retry
`pg_try_advisory_lock` (proposed default: 5s) and how quickly Postgres
notices a dead connection (`tcp_keepalives_*`/`statement_timeout` on the
Postgres side - operator-tunable, not something BOP needs to reimplement).

## What does *not* need to change

Crash recovery is already correct for this model with zero modification:
a leader that dies mid-job leaves that job `in_progress`; the new leader's
`FailOrphanedJobs` startup sweep (already built, already tested) marks it
`failed` on takeover, exactly like a single-controller restart today. HA
doesn't need a new recovery mechanism - it needs the existing one to run
on whichever process becomes leader next, which `bop controller`'s
existing startup sequence already does unconditionally.

Similarly, restic itself already tolerates being invoked from different
hosts against the same repository (that's how restic is designed to be
used - it's not a BOP-specific property). The only thing that must stay
true is "only the leader ever runs `forget --prune`," which falls out of
"only the leader runs jobs at all."

## Explicitly out of scope for this proposal

- **Active-active / sharding.** Rejected above - revisit only if
  single-controller throughput is a demonstrated bottleneck, not
  speculatively.
- **Split-brain beyond what Postgres's own consistency guarantees cover.**
  A leader that loses network connectivity to *target hosts* but keeps its
  Postgres connection alive would keep believing it's leader while unable
  to actually do anything useful - this is a real gap, not proposed to be
  solved here. Worth a health-check-driven voluntary lock release in a
  later iteration, not v1.
- **Automatic Postgres HA itself.** This proposal makes Postgres a
  required shared dependency for anyone opting into multi-controller mode;
  making *that* highly available is an operator concern (managed Postgres,
  Patroni, etc.), not something BOP takes on.
- **Migrating existing SQLite deployments automatically.** Opt-in only -
  single-controller SQLite deployments are unaffected and remain fully
  supported; nothing here deprecates that path.

## Open questions (need a decision before implementation starts)

1. Does a standby reject `POST /v1/backups` with a clear "not the leader"
   error, or transparently proxy it to whichever process currently holds
   the lock? Proxying is friendlier but adds real complexity (the standby
   needs to know the leader's address, not just that it isn't one).
2. Is PostgreSQL an acceptable new *required* dependency for anyone who
   wants HA, or does that need to stay optional somehow (e.g. keep SQLite
   viable for a lighter-weight HA story)? This proposal assumes requiring
   Postgres is fine since it's already the stated metadata direction, but
   that's worth confirming explicitly rather than assuming.
3. NATS vs. Redis vs. a Postgres-backed queue table (`SELECT ... FOR
   UPDATE SKIP LOCKED`) for the shared queue - existing docs lean
   NATS/Redis; a Postgres-backed queue would mean zero *additional*
   dependencies beyond what leader election already needs, at the cost of
   less throughput/feature headroom than a real broker. Worth an explicit
   choice, not an assumption.
