# Introduction

Backup Orchestration Platform (BOP) is a distributed orchestration system that
discovers infrastructure, executes backup workflows, verifies recoverability,
enforces retention policies, exports observability metrics, and coordinates
storage providers.

BOP is **not** a backup engine. It delegates storage to battle-tested tools like
Restic while providing an extensible orchestration layer that can support
heterogeneous infrastructure and future growth.

## The Five Core Questions

BOP exists to answer five questions for any piece of infrastructure:

1. **What** should be backed up?
2. **When** should it be backed up?
3. **How** should it be backed up?
4. **Where** should it be stored?
5. **Can it actually be restored?**

Everything else is a supporting detail.

## Data → Artifact → Repository

Most backup tools combine three distinct phases into one monolith. BOP separates them:

```
Data                Artifact                  Repository
(database,           (dump file, tar,          (Restic, Borg,
 filesystem)          snapshot stream)          S3 bucket)
```

- **Plugins** create artifacts from data.
- **Storage providers** store artifacts into repositories.
- The **Controller** orchestrates the workflow end-to-end.

Separation means you can change any layer independently. A new database plugin
does not force a storage rewrite. A new storage backend does not require
touching backup logic. The controller never needs to know how PostgreSQL works.
It only calls `Backup()`.

## Why Build BOP?

Current backup approaches fall into two patterns:

1. **Script → Storage** - fragile, hard to scale, no discoverability.
2. **Agent → Backup Server** - vendor-locked, complex, often lacks a unified view.

Neither scales well across heterogeneous environments. As infrastructure grows,
requirements quickly become more complex:

- Databases: PostgreSQL, MySQL, MariaDB, Informix, MongoDB, Redis
- Containers: Docker, Kubernetes
- Filesystems: Linux, Windows, NAS, Object Storage

The orchestration layer - scheduling, inventory, verification, policy - becomes
the hard part. BOP makes orchestration the product.

## Core Principles

### 1. Orchestration First

BOP owns the workflow. It does **not** own storage. Storage is accessed through
a well-defined interface. Initially that interface is backed by Restic. Later
it could be Borg, MinIO, S3, or a custom provider - no architectural change
required.

### 2. Everything is a Plugin

No hardcoded logic for any data source. Every backup source implements the same
contract:

```go
BackupPlugin interface {
    Discover() ([]Resource, error)
    Backup(Resource) (Artifact, error)
    Restore(Artifact) error
    Verify(Artifact) error
    Health() error
    Metadata() PluginMetadata
}
```

The controller treats all plugins identically, whether PostgreSQL, Docker, or a
custom application.

`Verify()` is a **structural sanity check**, not a duplicate of storage-level
integrity checking or a restore test. It runs immediately after `Backup()`,
before the artifact is checksummed, encrypted, or uploaded, and answers "is
this artifact well-formed" (e.g. is the dump file parseable, is the tar stream
not truncated) as cheaply as possible. This gives BOP three distinct, cheapest-first
verification tiers:

1. `plugin.Verify()` - is the artifact well-formed? (local, milliseconds)
2. `StorageProvider.Verify()` - did it survive the trip to storage? (checksums, repo integrity)
3. Restore test (`plugin.Restore()` into a temp location) - can it actually be restored? (expensive, run less often)

### 3. Storage is Abstract

Storage providers also share a contract:

```go
StorageProvider interface {
    Store(Artifact) (SnapshotID, error)
    Retrieve(SnapshotID, Artifact) error
    Verify(SnapshotID) error
    Delete(SnapshotID) error
    Snapshots() ([]Snapshot, error)
    ApplyRetention(Policy) error
}
```

Today: ResticProvider. Tomorrow: BorgProvider, S3Provider, etc.

### 4. Inventory Driven

Infrastructure is never discovered from shell scripts. A single inventory.yaml
defines all hosts, plugins, retention policies, and credentials.

Example:

```yaml
servers:
  prod-db:
    host: 192.168.1.10
    plugins:
      postgres:
      filesystem:
    retention:
      daily: 7
      weekly: 4
      monthly: 12
  staging:
    host: 192.168.1.20
    plugins:
      docker:
```

`plugins` is a map, not a list: a plugin with no configuration (like `docker`
above) has a null value, and a plugin that needs configuration (like
`postgres`) nests it under a `config` key - see the [Quickstart](03-getting-started/quickstart.md)
for a worked example.

Adding a new server means adding a YAML entry - not changing code. The
inventory format is designed to evolve from flat files to a database or
Kubernetes CRDs without changing the controller.

## Non-Goals

- BOP does not implement its own backup storage engine.

- BOP does not replace Restic, Borg, or similar tools - it builds on them.

- BOP does not provide a GUI in its initial release (a web UI is planned).

- BOP is not an all-in-one backup appliance.

With these principles in place, BOP scales from two VMs to hundreds of nodes
without redesigning its core.