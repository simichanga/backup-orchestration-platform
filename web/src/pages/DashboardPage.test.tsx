import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DashboardPage } from './DashboardPage'
import type { Host, Job } from '../api/types'

// ActivityChart renders recharts' ResponsiveContainer, which needs
// ResizeObserver - jsdom doesn't implement it. The chart's own data
// transform (bucketJobsByDay) is already covered in lib/activity.test.ts;
// this page's own job is producing the right stats/rows around it, which
// this mock lets us test without dragging recharts into jsdom.
vi.mock('../components/ActivityChart', () => ({
  ActivityChart: () => <div data-testid="activity-chart" />,
}))

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

function routedFetch(jobs: Job[]) {
  return vi.fn().mockImplementation(async (url: string) => {
    let body: unknown = []
    if (url.includes('/v1/hosts')) body = hosts
    else if (url.includes('/v1/jobs')) body = jobs
    else if (url.includes('/v1/events')) body = []
    return { ok: true, status: 200, statusText: 'OK', json: async () => body }
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

describe('DashboardPage', () => {
  it('counts queued/running and failed jobs separately from the total', async () => {
    vi.stubGlobal(
      'fetch',
      routedFetch([
        job({ id: 'a', status: 'queued' }),
        job({ id: 'b', status: 'in_progress' }),
        job({ id: 'c', status: 'failed' }),
        job({ id: 'd', status: 'completed' }),
      ]),
    )
    renderPage()

    // Hosts and jobs come from separate fetches (hostsState vs. jobsState)
    // - wait for all three stats together rather than gating on just one.
    await waitFor(() => {
      expect(screen.getByText('Queued or running').previousSibling).toHaveTextContent('2')
      expect(screen.getByText('Failed jobs').previousSibling).toHaveTextContent('1')
      expect(screen.getByText('Hosts in inventory').previousSibling).toHaveTextContent('1')
    })
  })

  it('shows only the 8 most recent jobs, most recent first', async () => {
    const jobs = Array.from({ length: 10 }, (_, i) =>
      job({ id: `job-${i}`, host: `host-${i}`, queuedAt: `2024-03-${String(i + 1).padStart(2, '0')}T00:00:00Z` }),
    )
    vi.stubGlobal('fetch', routedFetch(jobs))
    renderPage()

    const rows = await screen.findAllByRole('row')
    // one header row + 8 data rows (10 jobs, capped at 8)
    expect(rows.length).toBe(9)
    // host-9 (2024-03-10, the latest queuedAt) sorts first; host-0/host-1
    // (the two oldest) are pushed out of the top-8 window entirely.
    expect(rows[1]).toHaveTextContent('host-9')
    expect(screen.queryByText('host-0')).not.toBeInTheDocument()
  })

  it('renders the activity chart once jobs have loaded', async () => {
    vi.stubGlobal('fetch', routedFetch([job({})]))
    renderPage()
    expect(await screen.findByTestId('activity-chart')).toBeInTheDocument()
  })
})
