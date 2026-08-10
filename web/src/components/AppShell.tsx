import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../state/auth'
import styles from './AppShell.module.css'

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/hosts', label: 'Hosts' },
  { to: '/jobs', label: 'Jobs' },
  { to: '/snapshots', label: 'Snapshots' },
  { to: '/events', label: 'Events' },
]

export function AppShell() {
  const { disconnect } = useAuth()

  return (
    <div className={styles.shell}>
      <header className={styles.masthead}>
        <div className={styles.wordmark}>
          <svg viewBox="0 0 32 32" width="20" height="20" aria-hidden="true">
            <circle cx="16" cy="16" r="14" fill="none" stroke="var(--accent)" strokeWidth="2" />
            <path d="M9.5 16.5l4.2 4.2 8.8-10.2" fill="none" stroke="var(--text)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>BOP</span>
          <span className={styles.wordmarkSub}>ops console</span>
        </div>
        <button type="button" className={styles.disconnect} onClick={disconnect}>
          Disconnect
        </button>
      </header>
      <div className={styles.body}>
        <nav className={styles.nav} aria-label="Primary">
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) => `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <main className={styles.main}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
