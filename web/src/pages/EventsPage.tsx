import { motion } from 'framer-motion'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import { formatExact, formatRelative } from '../lib/format'
import { staggerTransition } from '../lib/motion'
import controls from '../styles/controls.module.css'
import table from '../styles/table.module.css'

export function EventsPage() {
  const [host, setHost] = useState('')
  const [jobId, setJobId] = useState('')
  const [appliedHost, setAppliedHost] = useState('')
  const [appliedJobId, setAppliedJobId] = useState('')
  const reduceMotion = usePrefersReducedMotion()

  const eventsState = useApi(
    () => api.listEvents({ host: appliedHost || undefined, jobId: appliedJobId || undefined, limit: 200 }),
    [appliedHost, appliedJobId],
  )

  function applyFilters() {
    setAppliedHost(host.trim())
    setAppliedJobId(jobId.trim())
  }

  return (
    <>
      <PageHeader title="Events" subtitle="Every structured event the controller has emitted, most recent first." />

      <form
        className={controls.toolbar}
        onSubmit={(e) => {
          e.preventDefault()
          applyFilters()
        }}
      >
        <input className={controls.input} placeholder="filter by host" value={host} onChange={(e) => setHost(e.target.value)} aria-label="Filter by host" />
        <input className={controls.input} placeholder="filter by job id" value={jobId} onChange={(e) => setJobId(e.target.value)} aria-label="Filter by job ID" />
        <button type="submit" className={controls.buttonSecondary}>
          Apply
        </button>
      </form>

      {eventsState.loading && <Loading label="Loading events" />}
      {eventsState.error && <ErrorNotice message={eventsState.error} onRetry={eventsState.reload} />}
      {eventsState.data && eventsState.data.length === 0 && <EmptyState title="No events match these filters" />}
      {eventsState.data && eventsState.data.length > 0 && (
        <div className={table.wrap}>
          <table className={table.table}>
            <thead>
              <tr>
                <th>Type</th>
                <th>Host</th>
                <th>Job</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {eventsState.data.map((event, i) => (
                <motion.tr key={i} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={staggerTransition(reduceMotion, i)}>
                  <td className={table.mono}>{event.type}</td>
                  <td>{event.host || '—'}</td>
                  <td className={table.mono}>
                    {event.jobId ? <Link to={`/jobs/${encodeURIComponent(event.jobId)}`}>{event.jobId.slice(0, 18)}…</Link> : '—'}
                  </td>
                  <td className={table.mono} title={formatExact(event.timestamp)}>
                    {formatRelative(event.timestamp)}
                  </td>
                </motion.tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
