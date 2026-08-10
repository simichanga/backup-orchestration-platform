import type { BopEvent } from '../api/types'
import styles from './Seal.module.css'

export type SealTier = 'none' | 'structural' | 'storage' | 'restored' | 'failed'

const TIER_FRACTION: Record<SealTier, number> = {
  none: 0,
  structural: 0.34,
  storage: 0.67,
  restored: 1,
  failed: 1,
}

const TIER_LABEL: Record<SealTier, string> = {
  none: 'Not yet verified',
  structural: 'Artifact produced',
  storage: 'Storage integrity confirmed',
  restored: 'Restore-tested',
  failed: 'Backup failed',
}

// Derives a job's verification tier from the real events BOP emits for it -
// see docs/02-architecture.md's three-tier verification model. This is not
// decorative: each ring fraction corresponds to a specific event type
// actually observed for the job, nothing is inferred or guessed.
export function sealTierForEvents(events: BopEvent[]): SealTier {
  const types = new Set(events.map((e) => e.type))
  if (types.has('BackupFailed')) return 'failed'
  if (types.has('RestoreVerificationCompleted')) return 'restored'
  if (types.has('RepositoryVerificationCompleted')) return 'storage'
  if (types.has('ArtifactCreated')) return 'structural'
  return 'none'
}

export function Seal({ tier, size = 40 }: { tier: SealTier; size?: number }) {
  const r = 15
  const c = 2 * Math.PI * r
  const fraction = TIER_FRACTION[tier]
  const dash = `${c * fraction} ${c}`
  const cls = tier === 'failed' ? styles.failed : tier === 'restored' ? styles.complete : styles.progress

  return (
    <span className={styles.wrap} style={{ width: size, height: size }} role="img" aria-label={TIER_LABEL[tier]} title={TIER_LABEL[tier]}>
      <svg viewBox="0 0 32 32" width={size} height={size}>
        <circle cx="16" cy="16" r={r} className={styles.track} fill="none" strokeWidth="2" />
        {fraction > 0 && (
          <circle
            cx="16"
            cy="16"
            r={r}
            className={cls}
            fill="none"
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={dash}
            transform="rotate(-90 16 16)"
          />
        )}
        {tier === 'restored' && (
          <path d="M10.5 16.5l3.8 3.8 7.2-8.6" className={styles.mark} fill="none" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        )}
        {tier === 'failed' && (
          <path d="M12 12l8 8M20 12l-8 8" className={styles.markFailed} fill="none" strokeWidth="2" strokeLinecap="round" />
        )}
      </svg>
    </span>
  )
}
