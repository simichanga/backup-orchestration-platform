import type { Job } from '../api/types'

export interface DayBucket {
  date: string
  label: string
  succeeded: number
  active: number
  failed: number
}

// Buckets jobs by the calendar day they were queued (the only timestamp
// jobSummary carries - there's no completedAt), using each job's current
// status. Real data only: a day with nothing queued is a real zero, not
// omitted, so the chart's x-axis stays a continuous timeline.
export function bucketJobsByDay(jobs: Job[], days: number): DayBucket[] {
  const buckets = new Map<string, DayBucket>()
  const now = new Date()

  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(now)
    d.setDate(d.getDate() - i)
    const date = d.toISOString().slice(0, 10)
    const label = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    buckets.set(date, { date, label, succeeded: 0, active: 0, failed: 0 })
  }

  for (const job of jobs) {
    const date = job.queuedAt.slice(0, 10)
    const bucket = buckets.get(date)
    if (!bucket) continue // outside the visible window
    if (job.status === 'completed') bucket.succeeded += 1
    else if (job.status === 'failed') bucket.failed += 1
    else bucket.active += 1
  }

  return Array.from(buckets.values())
}
