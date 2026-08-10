import { MotionConfig } from 'framer-motion'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ConnectPage } from './pages/ConnectPage'
import { DashboardPage } from './pages/DashboardPage'
import { EventsPage } from './pages/EventsPage'
import { HostsPage } from './pages/HostsPage'
import { JobDetailPage } from './pages/JobDetailPage'
import { JobsPage } from './pages/JobsPage'
import { SnapshotsPage } from './pages/SnapshotsPage'
import { AuthProvider, useAuth } from './state/auth'

function Router() {
  const { connected } = useAuth()

  if (!connected) return <ConnectPage />

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<DashboardPage />} />
        <Route path="hosts" element={<HostsPage />} />
        <Route path="jobs" element={<JobsPage />} />
        <Route path="jobs/:id" element={<JobDetailPage />} />
        <Route path="snapshots" element={<SnapshotsPage />} />
        <Route path="events" element={<EventsPage />} />
        <Route path="*" element={<DashboardPage />} />
      </Route>
    </Routes>
  )
}

export function App() {
  return (
    // reducedMotion="user" makes every animation in the app - here and in
    // any motion.* component below it - respect the OS-level "reduce
    // motion" preference automatically, without each component checking
    // it individually.
    <MotionConfig reducedMotion="user">
      <BrowserRouter>
        <AuthProvider>
          <Router />
        </AuthProvider>
      </BrowserRouter>
    </MotionConfig>
  )
}
