import { AnimatePresence, motion } from 'framer-motion'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
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
  const location = useLocation()
  const reduceMotion = usePrefersReducedMotion()

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
              {({ isActive }) => (
                <>
                  {isActive && (
                    <motion.span
                      layoutId="nav-active-indicator"
                      className={styles.navIndicator}
                      transition={reduceMotion ? { duration: 0 } : { type: 'spring', stiffness: 500, damping: 40 }}
                    />
                  )}
                  <span className={styles.navLabel}>{item.label}</span>
                </>
              )}
            </NavLink>
          ))}
        </nav>
        <main className={styles.main}>
          <AnimatePresence mode="wait" initial={false}>
            <motion.div
              key={location.pathname}
              initial={{ opacity: 0, y: 6 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -6 }}
              transition={reduceMotion ? { duration: 0 } : { duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
            >
              <Outlet />
            </motion.div>
          </AnimatePresence>
        </main>
      </div>
    </div>
  )
}
