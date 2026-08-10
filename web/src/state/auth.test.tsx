import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider, useAuth } from './auth'
import { api, clearToken, getToken, setToken } from '../api/client'

function mockFetch(status: number, body?: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: 'status text',
    json: async () => body ?? [],
  })
}

function setup() {
  return renderHook(() => useAuth(), { wrapper: AuthProvider })
}

beforeEach(() => {
  clearToken()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AuthProvider', () => {
  it('starts disconnected when there is no stored token', () => {
    const { result } = setup()
    expect(result.current.connected).toBe(false)
  })

  it('starts connected when a token already exists (e.g. a page reload)', () => {
    // AuthProvider seeds its initial state from getToken(), which reflects
    // whatever setToken last wrote - the same path a real page load takes
    // (client.ts reads sessionStorage itself, once, at module load).
    setToken('existing-token')
    const { result } = setup()
    expect(result.current.connected).toBe(true)
  })

  it('connects when the probe request succeeds', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('good-token')
    })
    expect(result.current.connected).toBe(true)
    expect(result.current.error).toBeNull()
    expect(getToken()).toBe('good-token')
  })

  it('rejects and clears the token when the probe comes back 401', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid token' }))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('bad-token')
    })
    expect(result.current.connected).toBe(false)
    expect(result.current.error).toMatch(/not accepted/i)
    expect(getToken()).toBeNull()
  })

  it('shows an unreachable-controller message for a non-ApiError failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network error')))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('some-token')
    })
    expect(result.current.connected).toBe(false)
    expect(result.current.error).toMatch(/could not reach/i)
  })

  it('disconnect clears the token and flips connected off', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('good-token')
    })
    expect(result.current.connected).toBe(true)

    act(() => {
      result.current.disconnect()
    })
    expect(result.current.connected).toBe(false)
    expect(getToken()).toBeNull()
  })

  // This is the exact bug manual Playwright testing caught once (see
  // docs/08-roadmap.md): a read-only token's 401 on a write-scoped
  // endpoint must not silently log the whole session out. triggerBackup
  // marks its request suppressGlobalUnauthorized, so it must NOT drop the
  // session - while an ordinary read request's 401 (a genuinely revoked or
  // wrong token) must.
  it('does not drop the session when a write-scoped request 401s (wrong scope, not a bad token)', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('read-only-token')
    })
    expect(result.current.connected).toBe(true)

    vi.stubGlobal('fetch', mockFetch(401, { error: 'read-only token' }))
    await act(async () => {
      await expect(api.triggerBackup({ host: 'demo-host', plugin: 'filesystem' })).rejects.toThrow()
    })
    expect(result.current.connected).toBe(true)
  })

  it('drops the session when a normal (non-suppressed) request 401s', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    const { result } = setup()
    await act(async () => {
      await result.current.connect('good-token')
    })
    expect(result.current.connected).toBe(true)

    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid token' }))
    await act(async () => {
      await expect(api.listHosts()).rejects.toThrow()
    })
    expect(result.current.connected).toBe(false)
  })
})
