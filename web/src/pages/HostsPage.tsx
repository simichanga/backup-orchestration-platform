import { api } from '../api/client'
import { PageHeader } from '../components/Page'
import { EmptyState, ErrorNotice, Loading } from '../components/States'
import { useApi } from '../hooks/useApi'
import table from '../styles/table.module.css'

export function HostsPage() {
  const { data, error, loading, reload } = useApi(() => api.listHosts(), [])

  return (
    <>
      <PageHeader title="Hosts" subtitle="Everything in the inventory, and what it's set up to back up." />
      {loading && <Loading label="Loading hosts" />}
      {error && <ErrorNotice message={error} onRetry={reload} />}
      {data && data.length === 0 && <EmptyState title="No hosts configured" body="Add one to inventory.yaml to see it here." />}
      {data && data.length > 0 && (
        <div className={table.wrap}>
          <table className={table.table}>
            <thead>
              <tr>
                <th>Name</th>
                <th>Address</th>
                <th>Plugins</th>
                <th>Schedule</th>
              </tr>
            </thead>
            <tbody>
              {data.map((host) => (
                <tr key={host.name}>
                  <td>{host.name}</td>
                  <td className={table.mono}>{host.host}</td>
                  <td className={table.mono}>{host.plugins.join(', ')}</td>
                  <td className={table.mono}>{host.schedule || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
