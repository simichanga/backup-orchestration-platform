import { useEffect, useState, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { InformixConfig } from '../api/types'
import { PageHeader } from '../components/Page'
import { ErrorNotice, Loading } from '../components/States'
import controls from '../styles/controls.module.css'
import styles from './PluginConfigPage.module.css'

const emptyConfig: InformixConfig = {
  host: '',
  port: 9088,
  username: '',
  passwordEnv: '',
  serverName: '',
  database: '',
  tables: [],
  agentPath: '',
}

// PluginConfigPage is deliberately informix-specific, not a generic
// plugin-config form: the route is named generically
// (/hosts/:host/plugins/:plugin/config) so a future plugin can reuse the
// path shape, but today only informix has a backend validator (see
// internal/api/plugin_config.go) - this page says so plainly rather than
// rendering a form for fields a PUT would just reject.
export function PluginConfigPage() {
  const { host = '', plugin = '' } = useParams()
  const [form, setForm] = useState<InformixConfig>(emptyConfig)
  const [tablesText, setTablesText] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (plugin !== 'informix') return
    let active = true
    setLoading(true)
    setLoadError(null)
    api
      .getPluginConfig(host, plugin)
      .then((cfg) => {
        if (!active) return
        setForm(cfg)
        setTablesText(cfg.tables.join(', '))
      })
      .catch((err) => {
        if (!active) return
        // A 404 means this host/plugin has never been configured through
        // the web before - that's the normal first-time state, not an
        // error to show: start from an empty form instead.
        if (err instanceof ApiError && err.status === 404) {
          setForm(emptyConfig)
          setTablesText('')
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
  }, [host, plugin])

  function update<K extends keyof InformixConfig>(key: K, value: InformixConfig[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setSaving(true)
    setSaveError(null)
    setSaved(false)
    const tables = tablesText
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean)
    try {
      const updated = await api.setPluginConfig(host, plugin, { ...form, tables })
      setForm(updated)
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

  if (plugin !== 'informix') {
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
        title={`Configure informix`}
        subtitle={`Connection and table settings for ${host}. Saves immediately - the next backup job for this host picks it up, no controller restart needed.`}
      />

      {loading && <Loading label="Loading configuration" />}
      {loadError && <ErrorNotice message={loadError} />}

      {!loading && !loadError && (
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

          <div className={styles.actions}>
            <button type="submit" className={controls.button} disabled={saving}>
              {saving ? 'Saving…' : 'Save configuration'}
            </button>
          </div>
        </form>
      )}
    </>
  )
}
