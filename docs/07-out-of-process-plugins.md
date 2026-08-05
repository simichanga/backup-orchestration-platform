# Out-of-Process Plugin Loading (Proposal)

**Status: proposal, not implemented.** Same status as
`docs/06-high-availability.md`: a design doc to react to, written
docs-first, not a description of shipped behavior. `config.PluginsConfig`
(`plugins.dir`, `plugins.allow_unsigned`) already exists in the config
schema but is unused by any code today - this proposal is what would
finally give those fields meaning.

## The problem, precisely

Phase 1's two plugins (postgres, filesystem) compile directly into the
`bop` binary (`docs/02-architecture.md`'s Plugin Engine section: "core
plugins are compiled into the bop binary; no separate install step").
Adding a third data source today means changing BOP's own source and
shipping a new `bop` binary. That's fine for a small, stable set of
built-in plugins, and deliberately deferred rather than solved in Phase 1
(per this project's repeated "don't build for a need that doesn't exist
yet" pattern this session). It stops being fine the moment someone wants
a plugin BOP's maintainers didn't write, or wants to update one plugin
without redeploying the whole controller.

## Recommendation: subprocess + gRPC (HashiCorp go-plugin pattern)

Three real options exist; two are worth ruling out explicitly rather than
silently:

- **Go's native `plugin` package (`.so` files).** Rejected: Linux/macOS
  only (no Windows - this project's own dev machine is Windows, per
  memory), and the host binary and plugin `.so` must be built with the
  *exact* same Go toolchain version or loading fails at runtime with an
  opaque error. That's an unacceptably fragile distribution story for
  third-party plugins built independently of BOP's own release cadence.
- **A bespoke stdin/stdout line-protocol.** Rejected: reinvents framing,
  versioning, and error handling that a well-established pattern already
  solves correctly. Not worth the maintenance burden for marginal
  simplicity gains.
- **Subprocess + gRPC over a local Unix socket/named pipe** (the pattern
  `github.com/hashicorp/go-plugin` implements, used by Terraform, Vault,
  Packer, Nomad). **Recommended.** BOP execs the plugin binary as a
  subprocess; the subprocess speaks a defined gRPC service back over a
  handshake-negotiated local socket; go-plugin handles process lifecycle,
  handshake versioning, and clean shutdown already, battle-tested at
  significant scale. This also opens the door to plugins written in any
  language with gRPC support, not just Go, without BOP committing to that
  now.

## What the wire protocol looks like

A new protobuf-defined gRPC service, structurally mirroring
`internal/plugin.BackupPlugin` almost exactly - `Discover`, `Backup`,
`Restore`, `Verify`, `Health`, `Metadata` become RPC methods instead of Go
interface methods. This is the key design property worth stating clearly:
**the controller's pipeline (`internal/controller/controller.go`) does not
change at all.** It already only depends on the `plugin.BackupPlugin`
interface, constructed via a `PluginFactory` function
(`func(srv, cfg, tempDir) (plugin.BackupPlugin, error)`) - see
`Controller.RegisterPlugin`. An out-of-process plugin needs exactly one
new thing: a `PluginFactory` implementation that, instead of constructing
a Go struct directly (like `postgres.NewFactory` does today), launches the
subprocess via go-plugin's client and returns a thin wrapper struct whose
six methods translate directly into gRPC calls. Everything upstream of
that factory - `RunJob`, verification, retention, events, metrics - needs
zero changes. This is the plugin abstraction doing exactly the job it was
designed for.

Protocol versioning: go-plugin's handshake already carries a protocol
version; BOP pins one and refuses to load a plugin that negotiates a
different one, with a clear error naming both versions - not a crash or a
silent partial-compatibility attempt.

## Lifecycle: per-job, not long-lived

Given Phase 1's single-serial-consumer model (one job runs at a time,
start to finish, before the next begins - see `docs/05-operations.md`),
the simplest correct lifecycle is: launch the plugin subprocess when
`PluginFactory` is called for a job (which already happens fresh per job
today, in-process plugins included - see `runPipeline`), use it for that
job's `Discover`/`Backup`/`Verify`/`Restore` calls, then terminate it
(go-plugin's `Client.Kill()`) once the job finishes. No pooling, no
keep-alive between jobs. This matches the existing in-process factory
pattern's actual behavior (a new `sshexec.SSHExecutor` is already
constructed fresh per job today) rather than introducing a new lifecycle
model alongside it. Revisit only if per-job subprocess startup overhead is
measured to actually matter - not assumed upfront.

## Security model: signing, not sandboxing

`plugins.allow_unsigned` already exists in the config schema, unused.
This proposal gives it a real meaning:

- A plugin binary must be signed (proposed: `minisign` - a single static
  binary, no PKI/CA infrastructure to stand up, consistent with this
  project's preference for the smallest dependency that solves the actual
  problem, same reasoning as choosing static bearer tokens over OIDC for
  the HTTP API).
- BOP verifies the signature against a trusted public key
  (`plugins.trusted_keys_file`, a new config field, `*_file`-only since a
  public key isn't a secret) before exec'ing anything found in
  `plugins.dir`. An unsigned or badly-signed binary is refused with a
  clear error, not silently skipped.
- `plugins.allow_unsigned: true` explicitly opts out, for local
  development/testing only - the same "no insecure fallback by default"
  posture already established for SSH host-key verification
  (`internal/sshexec`) and now the HTTP API's bearer-token requirement.
  Setting it should log a warning on every `bop controller` startup, not
  just silently work, so it's not accidentally left on in production.

**Explicitly not proposed for v1:** OS-level sandboxing beyond ordinary
process isolation (no seccomp, no gVisor, no container-per-plugin). A
plugin subprocess runs with the same OS-user privileges as `bop
controller` itself. Signing proves *provenance* (this binary is what the
trusted key-holder published), not that the binary is *safe* to run -
that's a real gap worth naming explicitly rather than implying signing
solves more than it does. Real sandboxing is a legitimate future
hardening step once out-of-process loading exists at all, not a
prerequisite for shipping it.

## Discovery

`plugins.dir` (already documented, unused) is scanned at `bop controller`
startup for executable files matching a naming convention (proposed:
`bop-plugin-<name>`, e.g. `bop-plugin-mysql`). Each is signature-verified
(above), then registered under `<name>` via the same
`Controller.RegisterPlugin` call in-process plugins already use - from the
controller's perspective, an out-of-process plugin and `postgres`/
`filesystem` are registered identically; only what's behind the
`PluginFactory` differs. A plugin name colliding with a built-in
(`postgres`, `filesystem`) is a startup error, not silent shadowing either
way.

## Explicitly out of scope for this proposal

- **Hot-reloading.** Picking up a new/updated plugin binary requires
  restarting `bop controller` (rescans `plugins.dir` at startup only).
  Live reload is real complexity for a benefit ("update one plugin
  without a controller restart") that hasn't been shown to matter yet.
- **A plugin registry/marketplace.** Out of scope entirely - `plugins.dir`
  is populated by whatever mechanism the operator already uses to deploy
  files to the controller host (config management, a deploy pipeline),
  not something BOP fetches or indexes itself.
- **Non-Go plugin implementations.** gRPC makes this *possible* later, but
  writing (or documenting how to write) a plugin in another language is
  not part of this proposal - Phase 1's plugin SDK stays Go-only for now.

## Open questions (need a decision before implementation starts)

1. Is `minisign` an acceptable new dependency (both for BOP - verifying
   signatures - and for anyone wanting to publish a plugin - signing
   them), or is a different signing mechanism (`cosign`/Sigstore, GPG)
   preferred? `cosign` is heavier but more standard in the container-image
   ecosystem if plugin distribution ever looks like that.
2. Should a signature-verification failure be fatal (`bop controller`
   refuses to start at all) or does it skip just that one plugin with a
   loud error, starting up with everything else registered? The latter is
   more available but means a bad plugin doesn't block controller startup
   the way a bad cron expression already does today (per
   `docs/02-architecture.md`'s scheduler notes) - worth being consistent
   with that existing precedent rather than deciding fresh.
3. Priority relative to `docs/06-high-availability.md` - both are real
   work; which matters more for what's actually planned to run BOP against
   next?
