import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api, clearToken, getToken, onUnauthorized, setToken } from './client'

function mockFetch(status: number, body?: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: 'status text',
    json: async () => body ?? {},
  })
}

beforeEach(() => {
  clearToken()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('token storage', () => {
  it('setToken persists to sessionStorage and getToken reflects it', () => {
    setToken('abc')
    expect(getToken()).toBe('abc')
    expect(sessionStorage.getItem('bop.token')).toBe('abc')
  })

  it('clearToken removes it from both module state and sessionStorage', () => {
    setToken('abc')
    clearToken()
    expect(getToken()).toBeNull()
    expect(sessionStorage.getItem('bop.token')).toBeNull()
  })
})

describe('401 handling', () => {
  // The auth provider's whole "drop back to the connect screen" behavior
  // hangs off this listener firing - a regression here means a revoked or
  // wrong token silently keeps rendering stale data instead of logging out.
  it('notifies onUnauthorized listeners on a 401', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid token' }))
    const listener = vi.fn()
    const unsubscribe = onUnauthorized(listener)
    await expect(api.listHosts()).rejects.toThrow(ApiError)
    expect(listener).toHaveBeenCalledOnce()
    unsubscribe()
  })

  // POST /v1/backups is the one request that must NOT log the whole
  // session out on 401, since that status also covers "valid token, wrong
  // scope" (see client.ts's RequestOptions doc comment) - this is the real
  // bug this project's manual testing caught once already.
  it('does not notify listeners for a request marked suppressGlobalUnauthorized', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'read-only token' }))
    const listener = vi.fn()
    const unsubscribe = onUnauthorized(listener)
    await expect(api.triggerBackup({ host: 'demo-host', plugin: 'filesystem' })).rejects.toThrow(ApiError)
    expect(listener).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('does not notify listeners on a successful response', async () => {
    vi.stubGlobal('fetch', mockFetch(200, []))
    const listener = vi.fn()
    const unsubscribe = onUnauthorized(listener)
    await api.listHosts()
    expect(listener).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('stops notifying a listener after it unsubscribes', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid token' }))
    const listener = vi.fn()
    const unsubscribe = onUnauthorized(listener)
    unsubscribe()
    await expect(api.listHosts()).rejects.toThrow(ApiError)
    expect(listener).not.toHaveBeenCalled()
  })
})

describe('error message extraction', () => {
  it('uses the server-provided error message when the body has one', async () => {
    vi.stubGlobal('fetch', mockFetch(500, { error: 'boom' }))
    await expect(api.listHosts()).rejects.toMatchObject({ status: 500, message: 'boom' })
  })

  it('falls back to statusText when the error body is not JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        json: async () => {
          throw new Error('not json')
        },
      }),
    )
    await expect(api.listHosts()).rejects.toMatchObject({ status: 500, message: 'Internal Server Error' })
  })
})

describe('request headers', () => {
  it('sends a Bearer Authorization header when a token is set', async () => {
    setToken('secret-token')
    const fetchMock = mockFetch(200, [])
    vi.stubGlobal('fetch', fetchMock)
    await api.listHosts()
    const headers = fetchMock.mock.calls[0][1].headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer secret-token')
  })

  it('omits the Authorization header when no token is set', async () => {
    const fetchMock = mockFetch(200, [])
    vi.stubGlobal('fetch', fetchMock)
    await api.listHosts()
    const headers = fetchMock.mock.calls[0][1].headers as Headers
    expect(headers.has('Authorization')).toBe(false)
  })
})
