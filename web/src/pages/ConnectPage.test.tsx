import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConnectPage } from './ConnectPage'
import { AuthProvider } from '../state/auth'

function mockFetch(status: number, body?: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: 'status text',
    json: async () => body ?? [],
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderPage() {
  return render(
    <AuthProvider>
      <ConnectPage />
    </AuthProvider>,
  )
}

describe('ConnectPage', () => {
  it('disables the submit button until a token is entered', async () => {
    renderPage()
    expect(screen.getByRole('button', { name: /connect/i })).toBeDisabled()

    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/api token/i), 'x')
    expect(screen.getByRole('button', { name: /connect/i })).toBeEnabled()
  })

  it('masks the token by default and reveals it on demand', async () => {
    renderPage()
    const input = screen.getByLabelText(/api token/i)
    expect(input).toHaveAttribute('type', 'password')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /show token/i }))
    expect(input).toHaveAttribute('type', 'text')
  })

  it('shows the auth provider error message for a rejected token', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid token' }))
    renderPage()
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/api token/i), 'bad-token')
    await user.click(screen.getByRole('button', { name: /connect/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/not accepted/i)
  })

  it('trims whitespace before connecting, and refuses whitespace-only input', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    renderPage()
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/api token/i), '   ')
    expect(screen.getByRole('button', { name: /connect/i })).toBeDisabled()
  })
})
