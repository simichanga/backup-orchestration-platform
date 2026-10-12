import { useEffect, useState, type ComponentType, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { FilesystemConfig, InformixConfig, PluginConfig, PostgresConfig } from '../api/types'
import { PageHeader } from '../components/Page'
import { ErrorNotice, Loading } from '../components/States'
import controls from '../styles/controls.module.css'
import styles from './PluginConfigPage.module.css'

const emptyInformixConfig: InformixConfig = {
  host: '',
  port: 9088,
  username: '',
  passwordEnv: '',
  serverName: '',
  database: '',
  tables: [],
  agentPath: '',
}

const emptyPostgresConfig: PostgresConfig = {
  username: '',
  passwordEnv: '',
  databases: [],
  container: '',
}

const emptyFilesystemConfig: FilesystemConfig = {
  paths: [],
  excludes: [],
}

function splitList(text: string): string[] {
  return text
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
}

// usePluginConfigForm is the load/save state machine shared by every
// plugin's config form below - fetching the stored config (treating a 404
// as "never configured," not an error), and submitting an update with the
// same write-scope-401 handling every save path needs. emptyConfig must be
// a stable reference (the module-level constants above, not a literal
// created on every render) since it's only read inside the effect, not a
// dependency of it.
//
// onConfigLoaded fires synchronously alongside setForm (same .then/.catch
// callback, same state-update batch) whenever a config is loaded or
// saved - not from a separate effect watching `form`. A caller with an
// array field rendered as a comma-separated text input (tables,
// databases, paths, ...) uses this to sync that text state in the same
// render pass as the form data, matching how a single state update
// behaves for callers that don't need it. Splitting that sync into its
// own `useEffect(() => ..., [form.someArray])` instead adds an extra
// render pass - which a direct `getByDisplayValue` assertion right after
// an awaited load won't wait for.
function usePluginConfigForm<T extends PluginConfig>(
  host: string,
  plugin: string,
  emptyConfig: T,
  onConfigLoaded?: (cfg: T) => void,
) {
  const [form, setForm] = useState<T>(emptyConfig)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError(null)
    api
      .getPluginConfig<T>(host, plugin)
      .then((cfg) => {
        if (!active) return
        setForm(cfg)
        onConfigLoaded?.(cfg)
      })
      .catch((err) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 404) {
          setForm(emptyConfig)
          onConfigLoaded?.(emptyConfig)
          return
        }
        setLoadError(err instanceof ApiError ? err.message : 'Could not reach the controller.')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
    // emptyConfig/onConfigLoaded are intentionally not dependencies - see the doc comment above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [host, plugin])

  async function save(payload: T) {
    setSaving(true)
    setSaveError(null)
    setSaved(false)
    try {
      const updated = await api.setPluginConfig<T>(host, plugin, payload)
      setForm(updated)
      onConfigLoaded?.(updated)
      setSaved(true)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setSaveError("This token can't save plugin config. Reconnect with a write token to do this.")
      } else if (err instanceof ApiError) {
        setSaveError(err.message)
      } else {
        setSaveError('Could not reach the controller.')
      }
    } finally {
      setSaving(false)
    }
  }

  return { form, setForm, loading, loadError, saving, saveError, saved, save }
}

function SaveFeedback({ saveError, saved }: { saveError: string | null; saved: boolean }) {
  return (
    <>
      {saveError && (
        <p className={styles.error} role="alert">
          {saveError}
        </p>
      )}
      {saved && (
        <p className={styles.success} role="status">
          Saved. The next backup job for this host will use it.
        </p>
      )}
    </>
  )
}

function InformixConfigForm({ host }: { host: string }) {
  const [tablesText, setTablesText] = useState('')
  const { form, setForm, loading, loadError, saving, saveError, saved, save } = usePluginConfigForm(
    host,
    'informix',
    emptyInformixConfig,
    (cfg) => setTablesText(cfg.tables.join(', ')),
  )

  function update<K extends keyof InformixConfig>(key: K, value: InformixConfig[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    save({ ...form, tables: splitList(tablesText) })
  }

  if (loading) return <Loading label="Loading configuration" />
  if (loadError) return <ErrorNotice message={loadError} />

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <label className={styles.label} htmlFor="ifx-host">
        Informix host
      </label>
      <input
        id="ifx-host"
        className={controls.input}
        value={form.host}
        onChange={(e) => update('host', e.target.value)}
        required
      />

      <label className={styles.label} htmlFor="ifx-port">
        Port
      </label>
      <input
        id="ifx-port"
        type="number"
        className={controls.input}
        value={form.port}
        onChange={(e) => update('port', Number(e.target.value))}
        required
      />

      <label className={styles.label} htmlFor="ifx-username">
        Username
      </label>
      <input
        id="ifx-username"
        className={controls.input}
        value={form.username}
        onChange={(e) => update('username', e.target.value)}
        required
      />

      <label className={styles.label} htmlFor="ifx-password-env">
        Password environment variable
      </label>
      <input
        id="ifx-password-env"
        className={controls.input}
        value={form.passwordEnv}
        onChange={(e) => update('passwordEnv', e.target.value)}
        required
        placeholder="e.g. IFX_BACKUP_PASSWORD"
      />
      <p className={styles.hint}>
        The password itself is never entered here - set this environment variable wherever "bop controller" runs.
      </p>

      <label className={styles.label} htmlFor="ifx-server-name">
        Informix server name
      </label>
      <input
        id="ifx-server-name"
        className={controls.input}
        value={form.serverName}
        onChange={(e) => update('serverName', e.target.value)}
        required
        placeholder="INFORMIXSERVER"
      />

      <label className={styles.label} htmlFor="ifx-database">
        Database
      </label>
      <input
        id="ifx-database"
        className={controls.input}
        value={form.database}
        onChange={(e) => update('database', e.target.value)}
        required
      />

      <label className={styles.label} htmlFor="ifx-tables">
        Tables to back up
      </label>
      <input
        id="ifx-tables"
        className={controls.input}
        value={tablesText}
        onChange={(e) => setTablesText(e.target.value)}
        required
        placeholder="customers, orders"
      />
      <p className={styles.hint}>Comma-separated. One backup resource per table.</p>

      <label className={styles.label} htmlFor="ifx-agent-path">
        ifx-agent binary path
      </label>
      <input
        id="ifx-agent-path"
        className={controls.input}
        value={form.agentPath}
        onChange={(e) => update('agentPath', e.target.value)}
        required
        placeholder="/usr/local/bin/ifx-agent"
      />
      <p className={styles.hint}>Path to the ifx-agent binary on whatever host runs "bop controller".</p>

      <SaveFeedback saveError={saveError} saved={saved} />

      <div className={styles.actions}>
        <button type="submit" className={controls.button} disabled={saving}>
          {saving ? 'Saving…' : 'Save configuration'}
        </button>
      </div>
    </form>
  )
}

function PostgresConfigForm({ host }: { host: string }) {
  const [databasesText, setDatabasesText] = useState('')
  const { form, setForm, loading, loadError, saving, saveError, saved, save } = usePluginConfigForm(
    host,
    'postgres',
    emptyPostgresConfig,
    (cfg) => setDatabasesText(cfg.databases.join(', ')),
  )

  function update<K extends keyof PostgresConfig>(key: K, value: PostgresConfig[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    save({ ...form, databases: splitList(databasesText) })
  }

  if (loading) return <Loading label="Loading configuration" />
  if (loadError) return <ErrorNotice message={loadError} />

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <label className={styles.label} htmlFor="pg-username">
        Username
      </label>
      <input
        id="pg-username"
        className={controls.input}
        value={form.username}
        onChange={(e) => update('username', e.target.value)}
        required
      />

      <label className={styles.label} htmlFor="pg-password-env">
        Password environment variable
      </label>
      <input
        id="pg-password-env"
        className={controls.input}
        value={form.passwordEnv}
        onChange={(e) => update('passwordEnv', e.target.value)}
        required
        placeholder="e.g. PG_BACKUP_PASSWORD"
      />
      <p className={styles.hint}>
        The password itself is never entered here - set this environment variable wherever "bop controller" runs.
      </p>

      <label className={styles.label} htmlFor="pg-databases">
        Databases to back up
      </label>
      <input
        id="pg-databases"
        className={controls.input}
        value={databasesText}
        onChange={(e) => setDatabasesText(e.target.value)}
        required
        placeholder="appdb, reporting"
      />
      <p className={styles.hint}>Comma-separated. One backup resource per database.</p>

      <label className={styles.label} htmlFor="pg-container">
        Docker container name
      </label>
      <input
        id="pg-container"
        className={controls.input}
        value={form.container}
        onChange={(e) => update('container', e.target.value)}
        placeholder="optional - leave blank if Postgres runs directly on the host"
      />
      <p className={styles.hint}>When set, pg_dump/psql run inside this container via "docker exec".</p>

      <SaveFeedback saveError={saveError} saved={saved} />

      <div className={styles.actions}>
        <button type="submit" className={controls.button} disabled={saving}>
          {saving ? 'Saving…' : 'Save configuration'}
        </button>
      </div>
    </form>
  )
}

function FilesystemConfigForm({ host }: { host: string }) {
  const [pathsText, setPathsText] = useState('')
  const [excludesText, setExcludesText] = useState('')
  const { loading, loadError, saving, saveError, saved, save } = usePluginConfigForm(
    host,
    'filesystem',
    emptyFilesystemConfig,
    (cfg) => {
      setPathsText(cfg.paths.join(', '))
      setExcludesText(cfg.excludes.join(', '))
    },
  )

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    save({ paths: splitList(pathsText), excludes: splitList(excludesText) })
  }

  if (loading) return <Loading label="Loading configuration" />
  if (loadError) return <ErrorNotice message={loadError} />

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <label className={styles.label} htmlFor="fs-paths">
        Paths to back up
      </label>
      <input
        id="fs-paths"
        className={controls.input}
        value={pathsText}
        onChange={(e) => setPathsText(e.target.value)}
        required
        placeholder="/var/lib/app, /etc/app"
      />
      <p className={styles.hint}>Comma-separated absolute paths on the target host. One backup resource per path.</p>

      <label className={styles.label} htmlFor="fs-excludes">
        Excludes
      </label>
      <input
        id="fs-excludes"
        className={controls.input}
        value={excludesText}
        onChange={(e) => setExcludesText(e.target.value)}
        placeholder="*.tmp, *.log"
      />
      <p className={styles.hint}>Comma-separated glob patterns. Optional.</p>

      <SaveFeedback saveError={saveError} saved={saved} />

      <div className={styles.actions}>
        <button type="submit" className={controls.button} disabled={saving}>
          {saving ? 'Saving…' : 'Save configuration'}
        </button>
      </div>
    </form>
  )
}

const pluginForms: Record<string, ComponentType<{ host: string }>> = {
  informix: InformixConfigForm,
  postgres: PostgresConfigForm,
  filesystem: FilesystemConfigForm,
}

// PluginConfigPage picks a form by the route's :plugin segment
// (/hosts/:host/plugins/:plugin/config). The route is named generically so
// a future plugin can reuse the path shape, but each plugin needs its own
// form above (matching its own backend validator in
// internal/api/plugin_config.go) - this page says so plainly for any
// plugin without one yet, rather than rendering a form for fields a PUT
// would just reject.
export function PluginConfigPage() {
  const { host = '', plugin = '' } = useParams()
  const Form = pluginForms[plugin]

  if (!Form) {
    return (
      <>
        <PageHeader title="Configure plugin" />
        <ErrorNotice message={`Web configuration isn't available for "${plugin}" yet - edit inventory.yaml directly.`} />
      </>
    )
  }

  return (
    <>
      <PageHeader
        title={`Configure ${plugin}`}
        subtitle={`Connection and resource settings for ${host}. Saves immediately - the next backup job for this host picks it up, no controller restart needed.`}
      />
      <Form host={host} />
    </>
  )
}
