import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Seal, sealTierForEvents } from './Seal'
import type { BopEvent, EventType } from '../api/types'

function event(type: EventType): BopEvent {
  return { type, jobId: 'job-1', host: 'demo-host', plugin: 'filesystem', resource: '', fields: {}, timestamp: '2024-03-10T00:00:00Z' }
}

describe('sealTierForEvents', () => {
  // The whole point of this component (per its own doc comment) is that
  // the ring reflects real observed events, not a guess from job status -
  // these map 1:1 to the three-tier verification model in
  // docs/02-architecture.md.
  it('is "none" when nothing has happened yet', () => {
    expect(sealTierForEvents([])).toBe('none')
  })

  it('is "structural" once an artifact exists', () => {
    expect(sealTierForEvents([event('BackupStarted'), event('ArtifactCreated')])).toBe('structural')
  })

  it('is "storage" once storage integrity is confirmed', () => {
    expect(sealTierForEvents([event('ArtifactCreated'), event('RepositoryVerificationCompleted')])).toBe('storage')
  })

  it('is "restored" once a restore test has completed', () => {
    expect(
      sealTierForEvents([event('ArtifactCreated'), event('RepositoryVerificationCompleted'), event('RestoreVerificationCompleted')]),
    ).toBe('restored')
  })

  it('is "failed" whenever BackupFailed appears, regardless of other events', () => {
    expect(sealTierForEvents([event('ArtifactCreated'), event('RepositoryVerificationCompleted'), event('BackupFailed')])).toBe('failed')
  })

  it('does not require events to arrive in order', () => {
    expect(sealTierForEvents([event('RestoreVerificationCompleted'), event('ArtifactCreated')])).toBe('restored')
  })
})

describe('Seal', () => {
  it('labels each tier accurately for assistive tech', () => {
    render(<Seal tier="storage" />)
    expect(screen.getByRole('img', { name: 'Storage integrity confirmed' })).toBeInTheDocument()
  })

  it('labels a failed backup distinctly from an in-progress one', () => {
    render(<Seal tier="failed" />)
    expect(screen.getByRole('img', { name: 'Backup failed' })).toBeInTheDocument()
  })
})
