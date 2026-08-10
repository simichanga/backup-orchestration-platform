import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TriggerBackupModal } from './TriggerBackupModal'
import type { Host } from '../api/types'

const hosts: Host[] = [{ name: 'demo-host', host: '127.0.0.1', plugins: ['filesystem'], schedule: '@daily' }]

function mockFetch(status: number, body?: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: 'status text',
    json: async () => body ?? {},
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', mockFetch(200, { id: 'job-123' }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderModal(onClose = vi.fn()) {
  return render(
    <MemoryRouter>
      <TriggerBackupModal open hosts={hosts} onClose={onClose} />
    </MemoryRouter>,
  )
}

describe('TriggerBackupModal', () => {
  it('shows the queued job id on a successful submit', async () => {
    const user = userEvent.setup()
    renderModal()
    await user.click(screen.getByRole('button', { name: /trigger backup/i }))
    expect(await screen.findByText('job-123')).toBeInTheDocument()
    expect(screen.getByText(/backup queued/i)).toBeInTheDocument()
  })

  // This is the exact scenario TESTING.md tells users to go check by hand
  // (connect with the read-only token, hit Trigger backup): a write-scoped
  // 401 must show an inline message and leave the dialog open, not silently
  // do nothing and not log the whole session out from under the user.
  it('shows a clear inline message, without closing, when the token lacks write scope', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'read-only token' }))
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderModal(onClose)

    await user.click(screen.getByRole('button', { name: /trigger backup/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/reconnect with a write token/i)
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('shows a generic unreachable message for a network failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network error')))
    const user = userEvent.setup()
    renderModal()

    await user.click(screen.getByRole('button', { name: /trigger backup/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not reach the controller/i)
  })
})
