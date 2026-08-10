import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { JobsPage } from './JobsPage'
import type { Host, Job } from '../api/types'

const hosts: Host[] = [{ name: 'demo-host', host: '127.0.0.1', plugins: ['filesystem'], schedule: '@daily' }]

function job(overrides: Partial<Job>): Job {
  return {
    id: 'job-1',
    host: 'demo-host',
    plugin: 'filesystem',
    policy: { daily: 7, weekly: 0, monthly: 0, yearly: 0 },
    status: 'completed',
    queuedAt: '2024-03-09T00:00:00Z',
    ...overrides,
  }
}

function routedFetch(jobsByUrl: (url: string) => Job[]) {
  return vi.fn().mockImplementation(async (url: string) => {
    const body = url.includes('/v1/hosts') ? hosts : jobsByUrl(url)
    return { ok: true, status: 200, statusText: 'OK', json: async () => body }
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', routedFetch(() => [job({ id: 'a' })]))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage() {
  return render(
    <MemoryRouter>
      <JobsPage />
    </MemoryRouter>,
  )
}

describe('JobsPage', () => {
  it('disables Trigger backup until hosts have loaded', async () => {
    renderPage()
    expect(screen.getByRole('button', { name: /trigger backup/i })).toBeDisabled()
    await waitFor(() => expect(screen.getByRole('button', { name: /trigger backup/i })).toBeEnabled())
  })

  it('re-fetches with the selected status when the filter changes', async () => {
    const fetchMock = routedFetch(() => [job({ id: 'a' })])
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('demo-host')

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText(/filter by status/i), 'failed')

    await waitFor(() => {
      const calls = fetchMock.mock.calls.map((c) => c[0] as string)
      expect(calls.some((u) => u.includes('/v1/jobs?status=failed'))).toBe(true)
    })
  })

  it('shows a filter-aware empty state', async () => {
    vi.stubGlobal('fetch', routedFetch(() => []))
    renderPage()
    expect(await screen.findByText(/no jobs match this filter/i)).toBeInTheDocument()
    expect(screen.getByText(/trigger a backup to see one here/i)).toBeInTheDocument()
  })
})
