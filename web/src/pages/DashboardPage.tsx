import { motion } from 'framer-motion'
import { lazy, Suspense, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { StatusPill } from '../components/StatusPill'
import { TriggerBackupModal } from '../components/TriggerBackupModal'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import { bucketJobsByDay } from '../lib/activity'
import { formatRelative } from '../lib/format'
import { staggerTransition } from '../lib/motion'
import controls from '../styles/controls.module.css'
import table from '../styles/table.module.css'
import styles from './DashboardPage.module.css'

const ACTIVITY_WINDOW_DAYS = 14

// Recharts is a meaningful chunk of bundle weight for a chart that only
// exists on this one page - load it lazily rather than paying for it on
// every route (Jobs/Snapshots/Events never need it).
const ActivityChart = lazy(() => import('../components/ActivityChart').then((m) => ({ default: m.ActivityChart })))

// Fleet status is exactly the kind of view where "reload to see if
// anything changed" is the wrong default - 5s is fast enough that a
// queued job visibly starts and finishes without a manual refresh.
const POLL_MS = 5000

export function DashboardPage() {
  const hostsState = useApi(() => api.listHosts(), [])
  const jobsState = useApi(() => api.listJobs(), [], POLL_MS)
  const eventsState = useApi(() => api.listEvents({ limit: 8 }), [], POLL_MS)
  const [showTrigger, setShowTrigger] = useState(false)
  const reduceMotion = usePrefersReducedMotion()

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

  const activity = useMemo(() => bucketJobsByDay(jobsState.data ?? [], ACTIVITY_WINDOW_DAYS), [jobsState.data])

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

      <section className={styles.activitySection}>
        <h2 className={styles.sectionTitle}>Activity, last {ACTIVITY_WINDOW_DAYS} days</h2>
        {jobsState.loading ? (
          <Loading label="Loading activity" />
        ) : (
          <Suspense fallback={<Loading label="Loading chart" />}>
            <ActivityChart data={activity} />
          </Suspense>
        )}
      </section>

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
                  {recentJobs.map((job, i) => (
                    <motion.tr key={job.id} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={staggerTransition(reduceMotion, i)}>
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
                    </motion.tr>
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
                <motion.li key={i} className={styles.feedItem} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={staggerTransition(reduceMotion, i)}>
                  <span className={styles.feedType}>{event.type}</span>
                  <span className={styles.feedMeta}>
                    {event.host} · {formatRelative(event.timestamp)}
                  </span>
                </motion.li>
              ))}
            </ul>
          )}
        </section>
      </div>

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
