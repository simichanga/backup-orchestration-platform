// Wire types mirror internal/api/handlers.go's *Summary DTOs exactly -
// see that file for the source of truth. Do not add fields the Go side
// doesn't actually send.

export type JobStatus = 'queued' | 'in_progress' | 'completed' | 'failed'

export interface Host {
  name: string
  host: string
  plugins: string[]
  schedule: string
}

export interface Policy {
  daily: number
  weekly: number
  monthly: number
  yearly: number
}

export interface Job {
  id: string
  host: string
  plugin: string
  policy: Policy
  status: JobStatus
  queuedAt: string
}

export interface Snapshot {
  id: string
  jobId: string
  host: string
  plugin: string
  size: number
  checksum: string
  createdAt: string
}

export type EventType =
  | 'BackupRequested'
  | 'BackupStarted'
  | 'BackupCompleted'
  | 'BackupFailed'
  | 'PluginDiscoveryStarted'
  | 'PluginDiscoveryCompleted'
  | 'ArtifactCreated'
  | 'ArtifactUploadStarted'
  | 'ArtifactUploadCompleted'
  | 'RepositoryVerificationStarted'
  | 'RepositoryVerificationCompleted'
  | 'RestoreVerificationStarted'
  | 'RestoreVerificationCompleted'
  | 'RetentionApplied'

export interface BopEvent {
  type: EventType
  jobId: string
  host: string
  plugin: string
  resource: string
  fields: Record<string, string>
  timestamp: string
}

export interface TriggerBackupRequest {
  host: string
  plugin: string
}

// Mirrors internal/api/plugin_config.go's *ConfigDTO types exactly. Web
// configuration is wired up for informix, postgres, and filesystem - the
// three registered BackupPlugin implementations.
export interface InformixConfig {
  host: string
  port: number
  username: string
  passwordEnv: string
  serverName: string
  database: string
  tables: string[]
  agentPath: string
}

export interface PostgresConfig {
  username: string
  passwordEnv: string
  databases: string[]
  container: string
}

export interface FilesystemConfig {
  paths: string[]
  excludes: string[]
}

export type PluginConfig = InformixConfig | PostgresConfig | FilesystemConfig
