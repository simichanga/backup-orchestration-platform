import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api/client'

interface ApiState<T> {
  data: T | null
  error: string | null
  loading: boolean
  reload: () => void
}

// Re-fetches whenever `deps` changes; ignores results from a stale, since
// -superseded request (e.g. a host filter changed mid-flight) so the UI
// never flickers back to an out-of-date answer.
export function useApi<T>(fetcher: () => Promise<T>, deps: unknown[]): ApiState<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const requestId = useRef(0)

  const load = useCallback(() => {
    const id = ++requestId.current
    setLoading(true)
    setError(null)
    fetcher()
      .then((result) => {
        if (id !== requestId.current) return
        setData(result)
        setLoading(false)
      })
      .catch((err) => {
        if (id !== requestId.current) return
        setError(err instanceof ApiError ? err.message : 'Could not reach the controller.')
        setLoading(false)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, error, loading, reload: load }
}
