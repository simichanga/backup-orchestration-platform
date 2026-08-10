import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { HostsPage } from './HostsPage'
import type { Host } from '../api/types'

function mockFetch(body: Host[]) {
  return vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK', json: async () => body })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('HostsPage', () => {
  it('lists every configured host', async () => {
    vi.stubGlobal('fetch', mockFetch([{ name: 'demo-host', host: '127.0.0.1', plugins: ['filesystem', 'postgres'], schedule: '@daily' }]))
    render(<HostsPage />)
    expect(await screen.findByText('demo-host')).toBeInTheDocument()
    expect(screen.getByText('filesystem, postgres')).toBeInTheDocument()
  })

  it('shows an em dash for a host with no schedule, not a blank cell', async () => {
    vi.stubGlobal('fetch', mockFetch([{ name: 'demo-host', host: '127.0.0.1', plugins: [], schedule: '' }]))
    render(<HostsPage />)
    await screen.findByText('demo-host')
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows the empty-inventory state', async () => {
    vi.stubGlobal('fetch', mockFetch([]))
    render(<HostsPage />)
    expect(await screen.findByText(/no hosts configured/i)).toBeInTheDocument()
  })
})
