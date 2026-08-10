import { useMemo } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { Seal, sealTierForEvents } from '../components/Seal'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { formatExact, formatRelative } from '../lib/format'
import styles from './JobDetailPage.module.css'

const POLL_MS = 5000

export function JobDetailPage() {
  const { id = '' } = useParams()
  // A single job is a cheap enough GET to just always poll while this
  // page is open - the bigger win is below, where the events poll stops
  // once there's nothing left to watch for.
  const jobState = useApi(() => api.getJob(id), [id], POLL_MS)
  const isTerminal = jobState.data?.status === 'completed' || jobState.data?.status === 'failed'
  const eventsState = useApi(() => api.listEvents({ jobId: id, limit: 200 }), [id], isTerminal ? undefined : POLL_MS)

  const tier = useMemo(() => sealTierForEvents(eventsState.data ?? []), [eventsState.data])
  const events = useMemo(() => [...(eventsState.data ?? [])].sort((a, b) => a.timestamp.localeCompare(b.timestamp)), [eventsState.data])

  if (jobState.error) {
    return (
      <>
        <PageHeader title="Job" />
        <ErrorNotice message={jobState.error} onRetry={jobState.reload} />
      </>
    )
  }

  const job = jobState.data

  return (
    <>
      <Link to="/jobs" className={styles.back}>
        ← All jobs
      </Link>

      {jobState.loading && <Loading label="Loading job" />}

      {job && (
        <>
          <div className={styles.header}>
            <Seal tier={tier} size={56} />
            <div>
              <h1 className={styles.title}>
                {job.host} <span className={styles.dim}>/</span> {job.plugin}
              </h1>
              <p className={styles.jobId}>{job.id}</p>
            </div>
            <div className={styles.headerStatus}>
              <StatusPill status={job.status} />
            </div>
          </div>

          <dl className={styles.facts}>
            <div>
              <dt>Queued</dt>
              <dd title={formatExact(job.queuedAt)}>{formatRelative(job.queuedAt)}</dd>
            </div>
            <div>
              <dt>Retention</dt>
              <dd>
                {job.policy.daily}d / {job.policy.weekly}w / {job.policy.monthly}m / {job.policy.yearly}y
              </dd>
            </div>
          </dl>

          <h2 className={styles.sectionTitle}>Timeline</h2>
          {eventsState.loading && <Loading label="Loading events" />}
          {eventsState.error && <ErrorNotice message={eventsState.error} onRetry={eventsState.reload} />}
          {events.length === 0 && !eventsState.loading && <EmptyState title="No events recorded for this job yet" />}
          {events.length > 0 && (
            <ol className={styles.timeline}>
              {events.map((event, i) => (
                <li key={i} className={styles.timelineItem}>
                  <span className={styles.timelineDot} aria-hidden="true" />
                  <div>
                    <p className={styles.timelineType}>{event.type}</p>
                    <p className={styles.timelineTime} title={formatExact(event.timestamp)}>
                      {formatRelative(event.timestamp)}
                    </p>
                    {Object.keys(event.fields ?? {}).length > 0 && (
                      <dl className={styles.timelineFields}>
                        {Object.entries(event.fields).map(([k, v]) => (
                          <div key={k}>
                            <dt>{k}</dt>
                            <dd>{v}</dd>
                          </div>
                        ))}
                      </dl>
                    )}
                  </div>
                </li>
              ))}
            </ol>
          )}
        </>
      )}
    </>
  )
}
