import type { BopEvent, Host, Job, Snapshot, TriggerBackupRequest } from './types'

const STORAGE_KEY = 'bop.token'

// Held in module state, not re-read from sessionStorage per request - the
// AuthProvider is the single writer, keeping every fetch call synchronous
// and testable without touching the DOM storage API directly.
let token: string | null = sessionStorage.getItem(STORAGE_KEY)

export function getToken(): string | null {
  return token
}

export function setToken(next: string): void {
  token = next
  sessionStorage.setItem(STORAGE_KEY, next)
}

export function clearToken(): void {
  token = null
  sessionStorage.removeItem(STORAGE_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// Fired whenever a request comes back 401 - the token that was presented
// is no longer valid (wrong, revoked, or never set). Listeners (the auth
// provider) drop the session and return to the connect screen; the failed
// request's own caller still gets a rejected promise so a specific action
// (like triggering a backup) can show its own inline message too.
type UnauthorizedListener = () => void
const unauthorizedListeners = new Set<UnauthorizedListener>()

export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener)
  return () => unauthorizedListeners.delete(listener)
}

interface RequestOptions {
  // POST /v1/backups can 401 for a token that is otherwise perfectly
  // valid - it just lacks write scope (see config.APIConfig's doc comment
  // on why read/write are separate token lists). The API has no way to
  // tell "invalid token" and "valid token, wrong scope" apart in its
  // response (both are a 401 "invalid token" - see internal/api/auth.go),
  // so that distinction has to be made here instead: only requests that
  // read a token's own validity (everything but the write action) drop
  // the session on 401. The caller still gets a rejected promise either
  // way, so triggerBackup's own catch block can show an inline message.
  suppressGlobalUnauthorized?: boolean
}

async function request<T>(path: string, init?: RequestInit, options?: RequestOptions): Promise<T> {
  const headers = new Headers(init?.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (init?.body) headers.set('Content-Type', 'application/json')

  const res = await fetch(path, { ...init, headers })

  if (res.status === 401 && !options?.suppressGlobalUnauthorized) {
    for (const listener of unauthorizedListeners) listener()
  }

  if (!res.ok) {
    let message = res.statusText
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Non-JSON error body - fall back to the status text above.
    }
    throw new ApiError(res.status, message)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  listHosts: () => request<Host[]>('/v1/hosts'),
  listJobs: (status?: string) =>
    request<Job[]>(`/v1/jobs${status ? `?status=${encodeURIComponent(status)}` : ''}`),
  getJob: (id: string) => request<Job>(`/v1/jobs/${encodeURIComponent(id)}`),
  listSnapshots: (host: string) => request<Snapshot[]>(`/v1/snapshots?host=${encodeURIComponent(host)}`),
  listEvents: (filter?: { jobId?: string; host?: string; limit?: number }) => {
    const params = new URLSearchParams()
    if (filter?.jobId) params.set('job_id', filter.jobId)
    if (filter?.host) params.set('host', filter.host)
    if (filter?.limit) params.set('limit', String(filter.limit))
    const qs = params.toString()
    return request<BopEvent[]>(`/v1/events${qs ? `?${qs}` : ''}`)
  },
  triggerBackup: (req: TriggerBackupRequest) =>
    request<Job>('/v1/backups', { method: 'POST', body: JSON.stringify(req) }, { suppressGlobalUnauthorized: true }),
}
