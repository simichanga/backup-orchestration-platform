import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError, api, clearToken, getToken, onUnauthorized, setToken } from '../api/client'

interface AuthState {
  connected: boolean
  connect: (token: string) => Promise<void>
  disconnect: () => void
  error: string | null
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(() => getToken() !== null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => onUnauthorized(() => setConnected(false)), [])

  const connect = useCallback(async (token: string) => {
    setError(null)
    setToken(token)
    try {
      // A token is only as good as a request that succeeds with it - probe
      // the cheapest read endpoint rather than trusting the pasted value.
      await api.listHosts()
      setConnected(true)
    } catch (err) {
      clearToken()
      setConnected(false)
      if (err instanceof ApiError && err.status === 401) {
        setError('That token was not accepted. Check it against api.tokens_file / api.write_tokens_file and try again.')
      } else {
        setError('Could not reach the controller. Confirm it is running and reachable, then try again.')
      }
    }
  }, [])

  const disconnect = useCallback(() => {
    clearToken()
    setConnected(false)
  }, [])

  return <AuthContext.Provider value={{ connected, connect, disconnect, error }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
