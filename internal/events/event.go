// Package events implements the Event System documented in
// docs/02-architecture.md: every meaningful controller action emits a
// structured event, pushed to subscribers (a log, a metrics pipeline, a
// notification system, an audit trail). Events require zero extra
// engineering inside plugins - the controller emits them automatically.
package events

import (
	"context"
	"time"
)

// Type identifies what happened. Names match
// docs/resources/event-system-flow.png, with one addition (TypeBackupFailed)
// the diagram doesn't show since it only depicts the happy path.
type Type string

const (
	// TypeBackupRequested is emitted by the scheduler when it creates a job,
	// before the job is even enqueued - distinct from TypeBackupStarted,
	// which the controller emits once it actually picks the job up. A job
	// triggered manually via "bop backup" skips this event since it never
	// goes through the scheduler.
	TypeBackupRequested Type = "BackupRequested"
	TypeBackupStarted   Type = "BackupStarted"
	TypeBackupCompleted Type = "BackupCompleted"
	TypeBackupFailed    Type = "BackupFailed"

	TypePluginDiscoveryStarted   Type = "PluginDiscoveryStarted"
	TypePluginDiscoveryCompleted Type = "PluginDiscoveryCompleted"

	TypeArtifactCreated         Type = "ArtifactCreated"
	TypeArtifactUploadStarted   Type = "ArtifactUploadStarted"
	TypeArtifactUploadCompleted Type = "ArtifactUploadCompleted"

	TypeRepositoryVerificationStarted   Type = "RepositoryVerificationStarted"
	TypeRepositoryVerificationCompleted Type = "RepositoryVerificationCompleted"

	TypeRestoreVerificationStarted   Type = "RestoreVerificationStarted"
	TypeRestoreVerificationCompleted Type = "RestoreVerificationCompleted"

	TypeRetentionApplied Type = "RetentionApplied"
)

// Event is a single structured event emitted by the controller.
type Event struct {
	Type      Type
	JobID     string
	Host      string
	Plugin    string
	Resource  string            // resource ID, when the event is resource-scoped
	Fields    map[string]string // event-specific payload (size, checksum, snapshot id, error, ...)
	Timestamp time.Time
}

// Publisher is the port every event subscriber implements (a log, a
// metrics pipeline, a notification system, an audit trail). Publish has no
// error return: emitting an event must never be able to fail a backup job,
// so there is nothing for a caller to propagate. An implementation that
// can fail (e.g. a network call) must handle that failure itself -
// logging it, retrying, or dropping the event - not surface it upward.
// Takes a context so a future network-based subscriber can honor
// cancellation/timeouts without an interface break.
type Publisher interface {
	Publish(ctx context.Context, event Event)
}
