import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatBytes, formatExact, formatRelative } from './format'

describe('formatBytes', () => {
  it('special-cases zero', () => {
    expect(formatBytes(0)).toBe('0 B')
  })

  it('does not decimal-format plain bytes', () => {
    expect(formatBytes(500)).toBe('500 B')
  })

  it('formats kilobytes with one decimal place', () => {
    expect(formatBytes(1536)).toBe('1.5 KB')
  })

  it('formats gigabytes', () => {
    expect(formatBytes(1073741824)).toBe('1.0 GB')
  })

  it('caps at TB rather than inventing a larger unit', () => {
    const oneYottabyte = 1024 ** 8
    expect(formatBytes(oneYottabyte)).toMatch(/ TB$/)
  })
})

describe('formatExact', () => {
  it('renders a date containing the source year', () => {
    expect(formatExact('2024-01-15T10:30:00Z')).toMatch(/2024/)
  })
})

describe('formatRelative', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2024-03-10T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('reports very recent past timestamps as "just now"', () => {
    expect(formatRelative('2024-03-10T11:59:58Z')).toBe('just now')
  })

  it('reports minutes in the past', () => {
    expect(formatRelative('2024-03-10T11:55:00Z')).toBe('5 minutes ago')
  })

  it('reports minutes in the future', () => {
    expect(formatRelative('2024-03-10T12:05:00Z')).toBe('in 5 minutes')
  })

  it('reports days in the past', () => {
    expect(formatRelative('2024-03-08T12:00:00Z')).toBe('2 days ago')
  })
})
