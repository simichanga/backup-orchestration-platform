import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PluginConfigPage } from './PluginConfigPage'
import type { InformixConfig } from '../api/types'

const storedConfig: InformixConfig = {
  host: '10.0.0.5',
  port: 9088,
  username: 'informix',
  passwordEnv: 'IFX_BACKUP_PASSWORD',
  serverName: 'informix',
  database: 'appdb',
  tables: ['customers', 'orders'],
  agentPath: '/usr/local/bin/ifx-agent',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage(host = 'prod-informix', plugin = 'informix') {
  return render(
    <MemoryRouter initialEntries={[`/hosts/${host}/plugins/${plugin}/config`]}>
      <Routes>
        <Route path="/hosts/:host/plugins/:plugin/config" element={<PluginConfigPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('PluginConfigPage', () => {
  it('loads and displays an existing stored config', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => storedConfig }))
    renderPage()

    expect(await screen.findByDisplayValue('10.0.0.5')).toBeInTheDocument()
    expect(screen.getByDisplayValue('appdb')).toBeInTheDocument()
    expect(screen.getByDisplayValue('customers, orders')).toBeInTheDocument()
  })

  it('treats a 404 as a first-time setup, not an error - starts from an empty form', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'no stored config' }) }),
    )
    renderPage('fresh-host')

    const hostInput = await screen.findByLabelText('Informix host')
    expect(hostInput).toHaveValue('')
    expect(screen.queryByText(/could not reach/i)).not.toBeInTheDocument()
  })

  it('shows a retryable load error for a non-404 failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, statusText: 'boom', json: async () => ({ error: 'boom' }) }),
    )
    renderPage()

    expect(await screen.findByText('boom')).toBeInTheDocument()
  })

  it('submits the form and shows a success message', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: true, status: 200, statusText: 'OK', json: async () => storedConfig }
      }
      return { ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'no stored config' }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

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
      return { ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'no stored config' }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

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

  it('shows a reconnect message, not a generic one, on a write-scope 401', async () => {
    const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return { ok: false, status: 401, statusText: 'Unauthorized', json: async () => ({ error: 'invalid token' }) }
      }
      return { ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'no stored config' }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

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

  it("shows a plain message instead of a form for a plugin that isn't informix", async () => {
    renderPage('prod-db', 'postgres')
    expect(await screen.findByText(/web configuration isn't available for "postgres"/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Informix host')).not.toBeInTheDocument()
  })
})
