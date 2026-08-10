import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EventsPage } from './EventsPage'

function mockFetch() {
  return vi.fn().mockImplementation(async () => ({ ok: true, status: 200, statusText: 'OK', json: async () => [] }))
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage() {
  return render(
    <MemoryRouter>
      <EventsPage />
    </MemoryRouter>,
  )
}

describe('EventsPage', () => {
  it('does not re-fetch while typing - only once Apply is submitted', async () => {
    const fetchMock = mockFetch()
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/filter by host/i), 'demo-host')
    // Still just the initial load - typing alone must not trigger a fetch.
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /apply/i }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const url = fetchMock.mock.calls[1][0] as string
    expect(url).toContain('host=demo-host')
  })

  it('trims filter values before applying them', async () => {
    const fetchMock = mockFetch()
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/filter by host/i), '  demo-host  ')
    await user.click(screen.getByRole('button', { name: /apply/i }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const url = fetchMock.mock.calls[1][0] as string
    expect(url).toContain('host=demo-host')
    expect(url).not.toContain('host=%20%20demo-host')
  })
})
