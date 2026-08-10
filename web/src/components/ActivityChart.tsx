import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { TooltipContentProps } from 'recharts'
import type { DayBucket } from '../lib/activity'
import styles from './ActivityChart.module.css'

const SERIES = [
  { key: 'succeeded' as const, label: 'Succeeded', color: 'var(--chart-succeeded)' },
  { key: 'active' as const, label: 'Queued / running', color: 'var(--chart-active)' },
  { key: 'failed' as const, label: 'Failed', color: 'var(--chart-failed)' },
]

function ChartTooltip({ active, payload, label }: TooltipContentProps) {
  if (!active || !payload?.length) return null
  const total = payload.reduce((sum, p) => sum + (typeof p.value === 'number' ? p.value : 0), 0)
  if (total === 0) return null
  return (
    <div className={styles.tooltip}>
      <p className={styles.tooltipLabel}>{label}</p>
      {SERIES.map((s) => {
        const entry = payload.find((p) => p.dataKey === s.key)
        const value = typeof entry?.value === 'number' ? entry.value : 0
        if (value === 0) return null
        return (
          <p key={s.key} className={styles.tooltipRow}>
            <span className={styles.swatch} style={{ background: s.color }} aria-hidden="true" />
            {s.label}
            <span className={styles.tooltipValue}>{value}</span>
          </p>
        )
      })}
    </div>
  )
}

export function ActivityChart({ data }: { data: DayBucket[] }) {
  const hasData = data.some((d) => d.succeeded + d.active + d.failed > 0)

  return (
    <div>
      <div className={styles.legend}>
        {SERIES.map((s) => (
          <span key={s.key} className={styles.legendItem}>
            <span className={styles.swatch} style={{ background: s.color }} aria-hidden="true" />
            {s.label}
          </span>
        ))}
      </div>
      <div className={styles.chart}>
        <ResponsiveContainer width="100%" height={160}>
          <BarChart data={data} margin={{ top: 4, right: 4, left: -20, bottom: 0 }} barCategoryGap={4}>
            <CartesianGrid vertical={false} stroke="var(--border)" />
            <XAxis
              dataKey="label"
              tickLine={false}
              axisLine={false}
              interval="preserveStartEnd"
              tick={{ fill: 'var(--text-dim)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
            />
            <YAxis
              allowDecimals={false}
              tickLine={false}
              axisLine={false}
              width={28}
              tick={{ fill: 'var(--text-dim)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
            />
            <Tooltip content={ChartTooltip} cursor={{ fill: 'var(--surface-raised)' }} />
            <Bar dataKey="succeeded" stackId="jobs" fill="var(--chart-succeeded)" stroke="var(--surface)" strokeWidth={2} />
            <Bar dataKey="active" stackId="jobs" fill="var(--chart-active)" stroke="var(--surface)" strokeWidth={2} />
            <Bar dataKey="failed" stackId="jobs" fill="var(--chart-failed)" stroke="var(--surface)" strokeWidth={2} radius={[4, 4, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
        {!hasData && <p className={styles.empty}>No jobs queued in this window yet.</p>}
      </div>
    </div>
  )
}
