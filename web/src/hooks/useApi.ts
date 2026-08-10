import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api/client'

interface ApiState<T> {
  data: T | null
  error: string | null
  loading: boolean
  reload: () => void
}

// Re-fetches whenever `deps` changes; ignores results from a stale,
// since-superseded request (e.g. a host filter changed mid-flight) so the
// UI never flickers back to an out-of-date answer.
//
// intervalMs, if given, re-fetches in the background on that cadence -
// silently, without flipping `loading` back to true or clearing good data
// on a transient failure, so a job doesn't visibly disappear just because
// one poll hiccupped. Polling pauses while the tab is hidden (Page
// Visibility API) and catches up with an immediate refresh when it's
// shown again, rather than firing requests nobody's looking at.
export function useApi<T>(fetcher: () => Promise<T>, deps: unknown[], intervalMs?: number): ApiState<T> {
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

  const refresh = useCallback(() => {
    const id = ++requestId.current
    fetcher()
      .then((result) => {
        if (id !== requestId.current) return
        setData(result)
      })
      .catch(() => {
        // Background refresh failures stay silent - the last good data
        // and any earlier foreground error (if that's what's showing)
        // are both better than replacing a working view with a blip.
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    if (!intervalMs) return

    let timer: ReturnType<typeof setInterval> | null = null
    const start = () => {
      if (timer) return
      timer = setInterval(refresh, intervalMs)
    }
    const stop = () => {
      if (!timer) return
      clearInterval(timer)
      timer = null
    }
    const onVisibility = () => {
      if (document.hidden) {
        stop()
      } else {
        refresh()
        start()
      }
    }

    if (!document.hidden) start()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      stop()
      document.removeEventListener('visibilitychange', onVisibility)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, refresh])

  return { data, error, loading, reload: load }
}
