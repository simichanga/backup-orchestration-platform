import { motion } from 'framer-motion'
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import { formatBytes, formatExact, formatRelative } from '../lib/format'
import { staggerTransition } from '../lib/motion'
import controls from '../styles/controls.module.css'
import table from '../styles/table.module.css'

export function SnapshotsPage() {
  const hostsState = useApi(() => api.listHosts(), [])
  const [host, setHost] = useState('')
  const reduceMotion = usePrefersReducedMotion()

  useEffect(() => {
    if (!host && hostsState.data?.length) setHost(hostsState.data[0].name)
  }, [host, hostsState.data])

  const snapshotsState = useApi(() => (host ? api.listSnapshots(host) : Promise.resolve([])), [host])

  return (
    <>
      <PageHeader title="Snapshots" subtitle="Stored artifacts, per host, as recorded by the storage provider." />

      <div className={controls.toolbar}>
        <select className={controls.select} value={host} onChange={(e) => setHost(e.target.value)} aria-label="Host">
          {(hostsState.data ?? []).map((h) => (
            <option key={h.name} value={h.name}>
              {h.name}
            </option>
          ))}
        </select>
      </div>

      {(hostsState.loading || snapshotsState.loading) && <Loading label="Loading snapshots" />}
      {snapshotsState.error && <ErrorNotice message={snapshotsState.error} onRetry={snapshotsState.reload} />}
      {snapshotsState.data && snapshotsState.data.length === 0 && (
        <EmptyState title="No snapshots for this host yet" body="They appear here once a backup job completes." />
      )}
      {snapshotsState.data && snapshotsState.data.length > 0 && (
        <div className={table.wrap}>
          <table className={table.table}>
            <thead>
              <tr>
                <th>Plugin</th>
                <th>Size</th>
                <th>Checksum</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {snapshotsState.data.map((snap, i) => (
                <motion.tr key={snap.id} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={staggerTransition(reduceMotion, i)}>
                  <td className={table.mono}>{snap.plugin}</td>
                  <td className={table.mono}>{formatBytes(snap.size)}</td>
                  <td className={table.mono} title={snap.checksum}>
                    {snap.checksum.slice(0, 12)}…
                  </td>
                  <td className={table.mono} title={formatExact(snap.createdAt)}>
                    {formatRelative(snap.createdAt)}
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
