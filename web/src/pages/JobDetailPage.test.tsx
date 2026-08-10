import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { JobDetailPage } from './JobDetailPage'
import type { BopEvent, Job } from '../api/types'

const baseJob: Job = {
  id: 'job-1',
  host: 'demo-host',
  plugin: 'filesystem',
  policy: { daily: 7, weekly: 0, monthly: 0, yearly: 0 },
  status: 'completed',
  queuedAt: '2024-03-09T00:00:00Z',
}

function event(type: BopEvent['type']): BopEvent {
  return { type, jobId: 'job-1', host: 'demo-host', plugin: 'filesystem', resource: '', fields: {}, timestamp: '2024-03-09T00:01:00Z' }
}

function routedFetch(job: Job, events: BopEvent[]) {
  return vi.fn().mockImplementation(async (url: string) => {
    const body = url.includes('/v1/events') ? events : job
    return { ok: true, status: 200, statusText: 'OK', json: async () => body }
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/jobs/job-1']}>
      <Routes>
        <Route path="/jobs/:id" element={<JobDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('JobDetailPage', () => {
  it("derives the seal tier from the job's real events, not its status alone", async () => {
    vi.stubGlobal('fetch', routedFetch(baseJob, [event('ArtifactCreated'), event('RepositoryVerificationCompleted')]))
    renderPage()

    await screen.findByText('job-1')
    // "Storage integrity confirmed" is the sealTierForEvents label for
    // exactly this pair of events - see components/Seal.test.tsx.
    expect(screen.getByRole('img', { name: 'Storage integrity confirmed' })).toBeInTheDocument()
  })

  it('renders the event timeline oldest first', async () => {
    const events = [
      { ...event('ArtifactCreated'), timestamp: '2024-03-09T00:02:00Z' },
      { ...event('BackupStarted'), timestamp: '2024-03-09T00:01:00Z' },
    ]
    vi.stubGlobal('fetch', routedFetch(baseJob, events))
    renderPage()

    const items = await screen.findAllByRole('listitem')
    expect(items[0]).toHaveTextContent('BackupStarted')
    expect(items[1]).toHaveTextContent('ArtifactCreated')
  })

  it('shows an empty timeline state when there are no events yet', async () => {
    vi.stubGlobal('fetch', routedFetch(baseJob, []))
    renderPage()
    expect(await screen.findByText(/no events recorded for this job yet/i)).toBeInTheDocument()
  })

  it('shows a retryable error notice when the job itself fails to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, statusText: 'boom', json: async () => ({ error: 'boom' }) }),
    )
    renderPage()
    expect(await screen.findByRole('button', { name: /retry/i })).toBeInTheDocument()
  })

  describe('event polling', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    function eventsCallCount(fetchMock: ReturnType<typeof routedFetch>) {
      return fetchMock.mock.calls.filter((c) => (c[0] as string).includes('/v1/events')).length
    }

    // The whole point of the isTerminal ? undefined : POLL_MS branch: once
    // a job is done, there's nothing left to watch for, so the *events*
    // poll should stop - the job itself still polls regardless (a single
    // GET is cheap enough to always refresh per the page's own comment),
    // so this has to count events calls specifically, not fetch calls overall.
    it('stops polling for new events once the job is terminal', async () => {
      const fetchMock = routedFetch({ ...baseJob, status: 'completed' }, [event('ArtifactCreated')])
      vi.stubGlobal('fetch', fetchMock)
      renderPage()

      await act(async () => {
        await Promise.resolve()
      })
      const callsAfterLoad = eventsCallCount(fetchMock)
      expect(callsAfterLoad).toBeGreaterThan(0)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(20000)
      })
      expect(eventsCallCount(fetchMock)).toBe(callsAfterLoad)
    })

    it('keeps polling for new events while the job is still running', async () => {
      const fetchMock = routedFetch({ ...baseJob, status: 'in_progress' }, [event('BackupStarted')])
      vi.stubGlobal('fetch', fetchMock)
      renderPage()

      await act(async () => {
        await Promise.resolve()
      })
      const callsAfterLoad = eventsCallCount(fetchMock)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000)
      })
      expect(eventsCallCount(fetchMock)).toBeGreaterThan(callsAfterLoad)
    })
  })
})
