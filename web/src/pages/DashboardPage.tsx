import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { StatusPill } from '../components/StatusPill'
import { TriggerBackupModal } from '../components/TriggerBackupModal'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { formatRelative } from '../lib/format'
import controls from '../styles/controls.module.css'
import table from '../styles/table.module.css'
import styles from './DashboardPage.module.css'

export function DashboardPage() {
  const hostsState = useApi(() => api.listHosts(), [])
  const jobsState = useApi(() => api.listJobs(), [])
  const eventsState = useApi(() => api.listEvents({ limit: 8 }), [])
  const [showTrigger, setShowTrigger] = useState(false)

  const stats = useMemo(() => {
    const jobs = jobsState.data ?? []
    return {
      hosts: hostsState.data?.length ?? 0,
      running: jobs.filter((j) => j.status === 'in_progress' || j.status === 'queued').length,
      failed: jobs.filter((j) => j.status === 'failed').length,
    }
  }, [hostsState.data, jobsState.data])

  const recentJobs = useMemo(
    () => [...(jobsState.data ?? [])].sort((a, b) => b.queuedAt.localeCompare(a.queuedAt)).slice(0, 8),
    [jobsState.data],
  )

  return (
    <>
      <PageHeader
        title="Fleet status"
        subtitle="What BOP has done, and what it's doing right now."
        actions={
          <button type="button" className={controls.button} onClick={() => setShowTrigger(true)} disabled={!hostsState.data?.length}>
            Trigger backup
          </button>
        }
      />

      <div className={styles.stats}>
        <div className={styles.stat}>
          <span className={styles.statValue}>{hostsState.loading ? '—' : stats.hosts}</span>
          <span className={styles.statLabel}>Hosts in inventory</span>
        </div>
        <div className={styles.stat}>
          <span className={styles.statValue}>{jobsState.loading ? '—' : stats.running}</span>
          <span className={styles.statLabel}>Queued or running</span>
        </div>
        <div className={`${styles.stat} ${stats.failed > 0 ? styles.statAlert : ''}`}>
          <span className={styles.statValue}>{jobsState.loading ? '—' : stats.failed}</span>
          <span className={styles.statLabel}>Failed jobs</span>
        </div>
      </div>

      <div className={styles.columns}>
        <section>
          <h2 className={styles.sectionTitle}>Recent jobs</h2>
          {jobsState.loading && <Loading label="Loading jobs" />}
          {jobsState.error && <ErrorNotice message={jobsState.error} onRetry={jobsState.reload} />}
          {jobsState.data && recentJobs.length === 0 && <EmptyState title="No jobs yet" body="Trigger a backup or wait for the next scheduled run." />}
          {recentJobs.length > 0 && (
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
                  {recentJobs.map((job) => (
                    <tr key={job.id}>
                      <td>
                        <Link to={`/jobs/${encodeURIComponent(job.id)}`} className={styles.rowLink}>
                          {job.host}
                        </Link>
                      </td>
                      <td className={table.mono}>{job.plugin}</td>
                      <td>
                        <StatusPill status={job.status} />
                      </td>
                      <td className={table.mono}>{formatRelative(job.queuedAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <section>
          <h2 className={styles.sectionTitle}>Recent events</h2>
          {eventsState.loading && <Loading label="Loading events" />}
          {eventsState.error && <ErrorNotice message={eventsState.error} onRetry={eventsState.reload} />}
          {eventsState.data && eventsState.data.length === 0 && <EmptyState title="No events yet" />}
          {eventsState.data && eventsState.data.length > 0 && (
            <ul className={styles.feed}>
              {eventsState.data.map((event, i) => (
                <li key={i} className={styles.feedItem}>
                  <span className={styles.feedType}>{event.type}</span>
                  <span className={styles.feedMeta}>
                    {event.host} · {formatRelative(event.timestamp)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      {showTrigger && hostsState.data && (
        <TriggerBackupModal
          hosts={hostsState.data}
          onClose={() => {
            setShowTrigger(false)
            jobsState.reload()
          }}
        />
      )}
    </>
  )
}
