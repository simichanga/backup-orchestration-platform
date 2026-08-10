import { motion } from 'framer-motion'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { StatusPill } from '../components/StatusPill'
import { TriggerBackupModal } from '../components/TriggerBackupModal'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import { formatRelative } from '../lib/format'
import { staggerTransition } from '../lib/motion'
import type { JobStatus } from '../api/types'
import controls from '../styles/controls.module.css'
import table from '../styles/table.module.css'

const POLL_MS = 5000

const STATUS_OPTIONS: { value: JobStatus | ''; label: string }[] = [
  { value: '', label: 'All statuses' },
  { value: 'queued', label: 'Queued' },
  { value: 'in_progress', label: 'Running' },
  { value: 'completed', label: 'Succeeded' },
  { value: 'failed', label: 'Failed' },
]

export function JobsPage() {
  const [status, setStatus] = useState<JobStatus | ''>('')
  const jobsState = useApi(() => api.listJobs(status || undefined), [status], POLL_MS)
  const hostsState = useApi(() => api.listHosts(), [])
  const [showTrigger, setShowTrigger] = useState(false)
  const reduceMotion = usePrefersReducedMotion()

  const jobs = [...(jobsState.data ?? [])].sort((a, b) => b.queuedAt.localeCompare(a.queuedAt))

  return (
    <>
      <PageHeader
        title="Jobs"
        subtitle="Every backup run BOP has queued, whether scheduled or triggered by hand."
        actions={
          <button type="button" className={controls.button} onClick={() => setShowTrigger(true)} disabled={!hostsState.data?.length}>
            Trigger backup
          </button>
        }
      />

      <div className={controls.toolbar}>
        <select className={controls.select} value={status} onChange={(e) => setStatus(e.target.value as JobStatus | '')} aria-label="Filter by status">
          {STATUS_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>

      {jobsState.loading && <Loading label="Loading jobs" />}
      {jobsState.error && <ErrorNotice message={jobsState.error} onRetry={jobsState.reload} />}
      {jobsState.data && jobs.length === 0 && (
        <EmptyState title="No jobs match this filter" body={status ? 'Try a different status, or trigger a backup.' : 'Trigger a backup to see one here.'} />
      )}
      {jobs.length > 0 && (
        <div className={table.wrap}>
          <table className={table.table}>
            <thead>
              <tr>
                <th>Host</th>
                <th>Plugin</th>
                <th>Status</th>
                <th>Queued</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job, i) => (
                <motion.tr key={job.id} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={staggerTransition(reduceMotion, i)}>
                  <td>
                    <Link to={`/jobs/${encodeURIComponent(job.id)}`}>{job.host}</Link>
                  </td>
                  <td className={table.mono}>{job.plugin}</td>
                  <td>
                    <StatusPill status={job.status} />
                  </td>
                  <td className={table.mono}>{formatRelative(job.queuedAt)}</td>
                </motion.tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TriggerBackupModal
        open={showTrigger}
        hosts={hostsState.data ?? []}
        onClose={() => {
          setShowTrigger(false)
          jobsState.reload()
        }}
      />
    </>
  )
}
