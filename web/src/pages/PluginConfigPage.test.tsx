import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PluginConfigPage } from './PluginConfigPage'
import type { FilesystemConfig, InformixConfig, PostgresConfig } from '../api/types'

const storedInformixConfig: InformixConfig = {
  host: '10.0.0.5',
  port: 9088,
  username: 'informix',
  passwordEnv: 'IFX_BACKUP_PASSWORD',
  serverName: 'informix',
  database: 'appdb',
  tables: ['customers', 'orders'],
  agentPath: '/usr/local/bin/ifx-agent',
}

const storedPostgresConfig: PostgresConfig = {
  username: 'backup_user',
  passwordEnv: 'PG_BACKUP_PASSWORD',
  databases: ['appdb', 'reporting'],
  container: 'my-postgres',
}

const storedFilesystemConfig: FilesystemConfig = {
  paths: ['/var/lib/app', '/etc/app'],
  excludes: ['*.tmp'],
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage(host: string, plugin: string) {
  return render(
    <MemoryRouter initialEntries={[`/hosts/${host}/plugins/${plugin}/config`]}>
      <Routes>
        <Route path="/hosts/:host/plugins/:plugin/config" element={<PluginConfigPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function notFoundResponse() {
  return { ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'no stored config' }) }
}

describe('PluginConfigPage - informix', () => {
  it('loads and displays an existing stored config', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => storedInformixConfig }),
    )
    renderPage('prod-informix', 'informix')

    expect(await screen.findByDisplayValue('10.0.0.5')).toBeInTheDocument()
    expect(screen.getByDisplayValue('appdb')).toBeInTheDocument()
    expect(screen.getByDisplayValue('customers, orders')).toBeInTheDocument()
  })

  it('treats a 404 as a first-time setup, not an error - starts from an empty form', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(notFoundResponse()))
    renderPage('fresh-host', 'informix')

    const hostInput = await screen.findByLabelText('Informix host')
    expect(hostInput).toHaveValue('')
    expect(screen.queryByText(/could not reach/i)).not.toBeInTheDocument()
  })

  it('shows a retryable load error for a non-404 failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, statusText: 'boom', json: async () => ({ error: 'boom' }) }),
    )
    renderPage('prod-informix', 'informix')

    expect(await screen.findByText('boom')).toBeInTheDocument()
  })

  it('submits the form and shows a success message', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, statusText: 'OK', json: async () => storedInformixConfig }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-informix', 'informix')

    await screen.findByLabelText('Informix host')
    fireEvent.change(screen.getByLabelText('Informix host'), { target: { value: '10.0.0.5' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Password environment variable'), { target: { value: 'IFX_BACKUP_PASSWORD' } })
    fireEvent.change(screen.getByLabelText('Informix server name'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'appdb' } })
    fireEvent.change(screen.getByLabelText('Tables to back up'), { target: { value: 'customers, orders' } })
    fireEvent.change(screen.getByLabelText('ifx-agent binary path'), { target: { value: '/usr/local/bin/ifx-agent' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('status')).toHaveTextContent(/saved/i)

    const putCall = fetchMock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PUT')
    expect(putCall).toBeDefined()
    const putBody = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(putBody.tables).toEqual(['customers', 'orders'])
  })

  it('shows an inline error and no success message when saving fails', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: false, status: 400, statusText: 'Bad Request', json: async () => ({ error: 'config.database is required' }) }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-informix', 'informix')

    await screen.findByLabelText('Informix host')
    fireEvent.change(screen.getByLabelText('Informix host'), { target: { value: '10.0.0.5' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Password environment variable'), { target: { value: 'IFX_BACKUP_PASSWORD' } })
    fireEvent.change(screen.getByLabelText('Informix server name'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'appdb' } })
    fireEvent.change(screen.getByLabelText('Tables to back up'), { target: { value: 'customers' } })
    fireEvent.change(screen.getByLabelText('ifx-agent binary path'), { target: { value: '/usr/local/bin/ifx-agent' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('config.database is required')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  // The write-scope-401 path lives in the shared usePluginConfigForm hook,
  // not in this form's own code - exercising it once here covers postgres
  // and filesystem too, so it isn't repeated in their describe blocks below.
  it('shows a reconnect message, not a generic one, on a write-scope 401', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: false, status: 401, statusText: 'Unauthorized', json: async () => ({ error: 'invalid token' }) }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-informix', 'informix')

    await screen.findByLabelText('Informix host')
    fireEvent.change(screen.getByLabelText('Informix host'), { target: { value: '10.0.0.5' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Password environment variable'), { target: { value: 'IFX_BACKUP_PASSWORD' } })
    fireEvent.change(screen.getByLabelText('Informix server name'), { target: { value: 'informix' } })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'appdb' } })
    fireEvent.change(screen.getByLabelText('Tables to back up'), { target: { value: 'customers' } })
    fireEvent.change(screen.getByLabelText('ifx-agent binary path'), { target: { value: '/usr/local/bin/ifx-agent' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/reconnect with a write token/i)
    })
  })
})

describe('PluginConfigPage - postgres', () => {
  it('loads and displays an existing stored config', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => storedPostgresConfig }),
    )
    renderPage('prod-db', 'postgres')

    expect(await screen.findByDisplayValue('backup_user')).toBeInTheDocument()
    expect(screen.getByDisplayValue('appdb, reporting')).toBeInTheDocument()
    expect(screen.getByDisplayValue('my-postgres')).toBeInTheDocument()
  })

  it('submits the form and shows a success message', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, statusText: 'OK', json: async () => storedPostgresConfig }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-db', 'postgres')

    await screen.findByLabelText('Username')
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'backup_user' } })
    fireEvent.change(screen.getByLabelText('Password environment variable'), { target: { value: 'PG_BACKUP_PASSWORD' } })
    fireEvent.change(screen.getByLabelText('Databases to back up'), { target: { value: 'appdb, reporting' } })
    fireEvent.change(screen.getByLabelText('Docker container name'), { target: { value: 'my-postgres' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('status')).toHaveTextContent(/saved/i)

    const putCall = fetchMock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PUT')
    const putBody = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(putBody.databases).toEqual(['appdb', 'reporting'])
  })

  it('shows an inline error and no success message when saving fails', async () => {
    // All required fields are filled (so HTML's own "required" validation
    // doesn't block the submit before the handler runs) - the failure
    // here is the backend rejecting the value, e.g. an unset env var.
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return {
          ok: false,
          status: 400,
          statusText: 'Bad Request',
          json: async () => ({ error: 'postgres: environment variable "PG_BACKUP_PASSWORD" (password_env) is not set' }),
        }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-db', 'postgres')

    await screen.findByLabelText('Username')
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'backup_user' } })
    fireEvent.change(screen.getByLabelText('Password environment variable'), { target: { value: 'PG_BACKUP_PASSWORD' } })
    fireEvent.change(screen.getByLabelText('Databases to back up'), { target: { value: 'appdb' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('is not set')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})

describe('PluginConfigPage - filesystem', () => {
  it('loads and displays an existing stored config', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => storedFilesystemConfig }),
    )
    renderPage('prod-fs', 'filesystem')

    expect(await screen.findByDisplayValue('/var/lib/app, /etc/app')).toBeInTheDocument()
    expect(screen.getByDisplayValue('*.tmp')).toBeInTheDocument()
  })

  it('treats a 404 as a first-time setup, not an error - starts from an empty form', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(notFoundResponse()))
    renderPage('fresh-host', 'filesystem')

    const pathsInput = await screen.findByLabelText('Paths to back up')
    expect(pathsInput).toHaveValue('')
  })

  it('submits the form and shows a success message', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, statusText: 'OK', json: async () => storedFilesystemConfig }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-fs', 'filesystem')

    await screen.findByLabelText('Paths to back up')
    fireEvent.change(screen.getByLabelText('Paths to back up'), { target: { value: '/var/lib/app, /etc/app' } })
    fireEvent.change(screen.getByLabelText('Excludes'), { target: { value: '*.tmp' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('status')).toHaveTextContent(/saved/i)

    const putCall = fetchMock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PUT')
    const putBody = JSON.parse((putCall![1] as RequestInit).body as string)
    expect(putBody.paths).toEqual(['/var/lib/app', '/etc/app'])
  })

  it('shows an inline error and no success message when saving fails', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: false, status: 400, statusText: 'Bad Request', json: async () => ({ error: 'filesystem: config.paths: "relative/path" must be an absolute path' }) }
      }
      return notFoundResponse()
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage('prod-fs', 'filesystem')

    await screen.findByLabelText('Paths to back up')
    fireEvent.change(screen.getByLabelText('Paths to back up'), { target: { value: 'relative/path' } })

    fireEvent.click(screen.getByRole('button', { name: /save configuration/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('must be an absolute path')
  })
})

describe('PluginConfigPage - unsupported plugin', () => {
  it("shows a plain message instead of a form for a plugin with no web configuration", async () => {
    // shellcmd is a shared command-quoting helper, not a registered
    // BackupPlugin - it has no ParseConfig and never will.
    renderPage('prod-db', 'shellcmd')
    expect(await screen.findByText(/web configuration isn't available for "shellcmd"/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Informix host')).not.toBeInTheDocument()
  })
})
