import { useState, type FormEvent } from 'react'
import { useAuth } from '../state/auth'
import styles from './ConnectPage.module.css'

export function ConnectPage() {
  const { connect, error } = useAuth()
  const [token, setTokenValue] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [reveal, setReveal] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!token.trim()) return
    setSubmitting(true)
    try {
      await connect(token.trim())
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.mark} aria-hidden="true">
          <svg viewBox="0 0 32 32" width="36" height="36">
            <circle cx="16" cy="16" r="14" fill="none" stroke="var(--accent)" strokeWidth="2" />
            <path
              d="M9.5 16.5l4.2 4.2 8.8-10.2"
              fill="none"
              stroke="var(--text)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <h1 className={styles.title}>BOP</h1>
        <p className={styles.subtitle}>ops console</p>

        <form onSubmit={handleSubmit} className={styles.form}>
          <label htmlFor="token" className={styles.label}>
            API token
          </label>
          <div className={styles.tokenRow}>
            <input
              id="token"
              className={styles.input}
              type={reveal ? 'text' : 'password'}
              value={token}
              onChange={(e) => setTokenValue(e.target.value)}
              placeholder="paste a read or write token"
              autoComplete="off"
              spellCheck={false}
              autoFocus
            />
            <button
              type="button"
              className={styles.reveal}
              onClick={() => setReveal((v) => !v)}
              aria-label={reveal ? 'Hide token' : 'Show token'}
            >
              {reveal ? 'Hide' : 'Show'}
            </button>
          </div>
          <p className={styles.hint}>
            Held only for this browser tab. A write token can also trigger backups; a read token can only view.
          </p>
          {error && (
            <p className={styles.error} role="alert">
              {error}
            </p>
          )}
          <button type="submit" className={styles.submit} disabled={submitting || !token.trim()}>
            {submitting ? 'Connecting…' : 'Connect'}
          </button>
        </form>
      </div>
    </div>
  )
}
