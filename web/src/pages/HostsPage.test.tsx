import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { HostsPage } from './HostsPage'
import type { Host } from '../api/types'

function mockFetch(body: Host[]) {
  return vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => body })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

// HostsPage now renders a <Link> (the "Configure Informix" action) per
// row, which needs a Router context - the same reason JobDetailPage's
// tests wrap render() in MemoryRouter.
function renderHostsPage() {
  return render(
    <MemoryRouter>
      <HostsPage />
    </MemoryRouter>,
  )
}

describe('HostsPage', () => {
  it('lists every configured host', async () => {
    vi.stubGlobal('fetch', mockFetch([{ name: 'demo-host', host: '127.0.0.1', plugins: ['filesystem', 'postgres'], schedule: '@daily' }]))
    renderHostsPage()
    expect(await screen.findByText('demo-host')).toBeInTheDocument()
    expect(screen.getByText('filesystem, postgres')).toBeInTheDocument()
  })

  it('shows an em dash for a host with no schedule, not a blank cell', async () => {
    vi.stubGlobal('fetch', mockFetch([{ name: 'demo-host', host: '127.0.0.1', plugins: [], schedule: '' }]))
    renderHostsPage()
    await screen.findByText('demo-host')
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows the empty-inventory state', async () => {
    vi.stubGlobal('fetch', mockFetch([]))
    renderHostsPage()
    expect(await screen.findByText(/no hosts configured/i)).toBeInTheDocument()
  })

  it('offers a Configure Informix link for every host', async () => {
    vi.stubGlobal('fetch', mockFetch([{ name: 'demo-host', host: '127.0.0.1', plugins: [], schedule: '' }]))
    renderHostsPage()
    const link = await screen.findByRole('link', { name: 'Configure Informix' })
    expect(link).toHaveAttribute('href', '/hosts/demo-host/plugins/informix/config')
  })
})
