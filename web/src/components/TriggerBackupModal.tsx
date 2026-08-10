import { AnimatePresence, motion } from 'framer-motion'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { Host } from '../api/types'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import styles from './TriggerBackupModal.module.css'

export function TriggerBackupModal({ open, hosts, onClose }: { open: boolean; hosts: Host[]; onClose: () => void }) {
  const [host, setHost] = useState(hosts[0]?.name ?? '')
  const [plugin, setPlugin] = useState(hosts[0]?.plugins[0] ?? '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [queuedJobId, setQueuedJobId] = useState<string | null>(null)
  const navigate = useNavigate()
  const reduceMotion = usePrefersReducedMotion()

  const selectedHost = hosts.find((h) => h.name === host)
  const plugins = selectedHost?.plugins ?? []

  // The dialog now stays mounted (AnimatePresence needs that to animate
  // the exit), so its own state has to be reset on each open rather than
  // relying on unmount/remount to do it for free.
  useEffect(() => {
    if (open) {
      setHost(hosts[0]?.name ?? '')
      setPlugin(hosts[0]?.plugins[0] ?? '')
      setSubmitting(false)
      setError(null)
      setQueuedJobId(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  useEffect(() => {
    if (plugins.length && !plugins.includes(plugin)) setPlugin(plugins[0])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [host])

  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  async function handleSubmit() {
    if (!host || !plugin) return
    setSubmitting(true)
    setError(null)
    try {
      const job = await api.triggerBackup({ host, plugin })
      setQueuedJobId(job.id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError("This token can't trigger backups. Reconnect with a write token to do this.")
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Could not reach the controller.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          className={styles.backdrop}
          onMouseDown={(e) => e.target === e.currentTarget && onClose()}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={reduceMotion ? { duration: 0 } : { duration: 0.15 }}
        >
          <motion.div
            className={styles.dialog}
            role="dialog"
            aria-modal="true"
            aria-labelledby="trigger-backup-title"
            initial={{ opacity: 0, y: 8, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 8, scale: 0.98 }}
            transition={reduceMotion ? { duration: 0 } : { duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
          >
            {queuedJobId ? (
              <>
                <h2 id="trigger-backup-title" className={styles.title}>
                  Backup queued
                </h2>
                <p className={styles.body}>
                  Job <span className={styles.jobId}>{queuedJobId}</span> is queued for the controller's next open slot.
                </p>
                <div className={styles.actions}>
                  <button type="button" className={styles.secondary} onClick={onClose}>
                    Close
                  </button>
                  <button
                    type="button"
                    className={styles.primary}
                    onClick={() => {
                      navigate(`/jobs/${encodeURIComponent(queuedJobId)}`)
                      onClose()
                    }}
                  >
                    View job
                  </button>
                </div>
              </>
            ) : (
              <>
                <h2 id="trigger-backup-title" className={styles.title}>
                  Trigger a backup
                </h2>
                <p className={styles.body}>Runs immediately, outside the regular schedule.</p>

                <label className={styles.label} htmlFor="trigger-host">
                  Host
                </label>
                <select id="trigger-host" className={styles.select} value={host} onChange={(e) => setHost(e.target.value)} autoFocus>
                  {hosts.map((h) => (
                    <option key={h.name} value={h.name}>
                      {h.name}
                    </option>
                  ))}
                </select>

                <label className={styles.label} htmlFor="trigger-plugin">
                  Plugin
                </label>
                <select id="trigger-plugin" className={styles.select} value={plugin} onChange={(e) => setPlugin(e.target.value)}>
                  {plugins.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>

                {error && (
                  <p className={styles.error} role="alert">
                    {error}
                  </p>
                )}

                <div className={styles.actions}>
                  <button type="button" className={styles.secondary} onClick={onClose}>
                    Cancel
                  </button>
                  <button type="button" className={styles.primary} onClick={handleSubmit} disabled={submitting || !host || !plugin}>
                    {submitting ? 'Queuing…' : 'Trigger backup'}
                  </button>
                </div>
              </>
            )}
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
