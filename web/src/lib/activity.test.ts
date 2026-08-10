import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { bucketJobsByDay } from './activity'
import type { Job } from '../api/types'

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

describe('bucketJobsByDay', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2024-03-10T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('produces one bucket per day in the window, oldest first', () => {
    const buckets = bucketJobsByDay([], 3)
    expect(buckets.map((b) => b.date)).toEqual(['2024-03-08', '2024-03-09', '2024-03-10'])
  })

  it('keeps a real zero for a day with no jobs, rather than omitting it', () => {
    const buckets = bucketJobsByDay([], 3)
    expect(buckets.every((b) => b.succeeded === 0 && b.active === 0 && b.failed === 0)).toBe(true)
  })

  it('buckets each job by its queued day and status', () => {
    const jobs = [
      job({ id: 'a', status: 'completed', queuedAt: '2024-03-09T08:00:00Z' }),
      job({ id: 'b', status: 'failed', queuedAt: '2024-03-09T09:00:00Z' }),
      job({ id: 'c', status: 'queued', queuedAt: '2024-03-10T10:00:00Z' }),
      job({ id: 'd', status: 'in_progress', queuedAt: '2024-03-10T11:00:00Z' }),
    ]
    const buckets = bucketJobsByDay(jobs, 3)
    const mar9 = buckets.find((b) => b.date === '2024-03-09')!
    const mar10 = buckets.find((b) => b.date === '2024-03-10')!

    expect(mar9).toMatchObject({ succeeded: 1, failed: 1, active: 0 })
    // both 'queued' and 'in_progress' count as "active" - anything that
    // isn't a terminal completed/failed status.
    expect(mar10).toMatchObject({ succeeded: 0, failed: 0, active: 2 })
  })

  it('drops jobs queued outside the visible window instead of erroring', () => {
    const jobs = [job({ queuedAt: '2024-01-01T00:00:00Z' })]
    const buckets = bucketJobsByDay(jobs, 3)
    expect(buckets.every((b) => b.succeeded === 0)).toBe(true)
  })
})
