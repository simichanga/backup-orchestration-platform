import type { JobStatus } from '../api/types'
import styles from './StatusPill.module.css'

const LABEL: Record<JobStatus, string> = {
  queued: 'Queued',
  in_progress: 'Running',
  completed: 'Succeeded',
  failed: 'Failed',
}

export function StatusPill({ status }: { status: JobStatus }) {
  return (
    <span className={`${styles.pill} ${styles[status]}`}>
      <span className={styles.dot} aria-hidden="true" />
      {LABEL[status]}
    </span>
  )
}
