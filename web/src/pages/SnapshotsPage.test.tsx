import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SnapshotsPage } from './SnapshotsPage'
import type { Host, Snapshot } from '../api/types'

const hosts: Host[] = [
  { name: 'host-a', host: '10.0.0.1', plugins: ['filesystem'], schedule: '@daily' },
  { name: 'host-b', host: '10.0.0.2', plugins: ['postgres'], schedule: '@daily' },
]

function snapshot(overrides: Partial<Snapshot>): Snapshot {
  return { id: 'snap-1', jobId: 'job-1', host: 'host-a', plugin: 'filesystem', size: 1024, checksum: 'abc123', createdAt: '2024-03-09T00:00:00Z', ...overrides }
}

function routedFetch(snapshotsByHost: Record<string, Snapshot[]>) {
  return vi.fn().mockImplementation(async (url: string) => {
    if (url.includes('/v1/hosts')) return { ok: true, status: 200, statusText: 'OK', json: async () => hosts }
    const match = /host=([^&]+)/.exec(url)
    const host = match ? decodeURIComponent(match[1]) : ''
    return { ok: true, status: 200, statusText: 'OK', json: async () => snapshotsByHost[host] ?? [] }
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SnapshotsPage', () => {
  it('auto-selects the first host once the inventory loads, and fetches its snapshots', async () => {
    vi.stubGlobal('fetch', routedFetch({ 'host-a': [snapshot({})] }))
    render(<SnapshotsPage />)

    await waitFor(() => expect(screen.getByLabelText(/host/i)).toHaveValue('host-a'))
    expect(await screen.findByText('abc123…')).toBeInTheDocument()
  })

  it('shows a per-host empty state', async () => {
    vi.stubGlobal('fetch', routedFetch({}))
    render(<SnapshotsPage />)
    expect(await screen.findByText(/no snapshots for this host yet/i)).toBeInTheDocument()
  })
})
