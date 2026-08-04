// Package core defines the domain types shared across plugin, storage, and
// queue ports. Keeping them here avoids import cycles between those packages.
package core

import "time"

// Resource is a single backed-up-able unit discovered by a plugin
// (e.g. one database, one filesystem path).
type Resource struct {
	ID     string
	Name   string
	Labels map[string]string
}

// Artifact is the output of a plugin's Backup call: a dump file, tar stream,
// or snapshot, plus the metadata the controller attaches as it moves through
// the pipeline (checksum, encryption).
type Artifact struct {
	ResourceID string
	Path       string
	Size       int64
	Checksum   string
	Encrypted  bool
	CreatedAt  time.Time
}

// PluginMetadata identifies a plugin implementation and its version.
type PluginMetadata struct {
	Name    string
	Version string
}

// SnapshotID identifies a stored artifact within a StorageProvider's repository.
type SnapshotID string

// Snapshot is a stored artifact's metadata as tracked by a StorageProvider.
type Snapshot struct {
	ID        SnapshotID
	Host      string
	Plugin    string
	Size      int64
	Checksum  string
	CreatedAt time.Time
}

// Policy is a retention policy applied by a StorageProvider.
type Policy struct {
	Daily   int `mapstructure:"daily" yaml:"daily"`
	Weekly  int `mapstructure:"weekly" yaml:"weekly"`
	Monthly int `mapstructure:"monthly" yaml:"monthly"`
	Yearly  int `mapstructure:"yearly" yaml:"yearly"`
}

// Verification configures the optional restore-test pipeline step. It is
// used both as the global default (config.yaml, loaded via Viper) and as a
// per-host override (inventory.yaml, loaded directly via yaml.v3) - hence
// carrying both struct tags.
type Verification struct {
	Enabled   bool   `mapstructure:"enabled" yaml:"enabled"`
	TargetDir string `mapstructure:"target_dir" yaml:"target_dir"`
}

// JobStatus is the lifecycle state of a Job as tracked by the metadata service.
type JobStatus string

const (
	JobStatusQueued     JobStatus = "queued"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// Job is a request to back up a specific host-plugin combination, created by
// the scheduler and consumed by the controller via a Queue.
type Job struct {
	ID       string
	Host     string
	Plugin   string
	Policy   Policy
	Status   JobStatus
	QueuedAt time.Time
}
