import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useApi } from './useApi'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (err: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useApi', () => {
  it('loads on mount and exposes the result', async () => {
    const fetcher = vi.fn().mockResolvedValue(['a'])
    const { result } = renderHook(() => useApi(fetcher, []))

    expect(result.current.loading).toBe(true)
    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.data).toEqual(['a'])
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  // The whole reason `requestId` exists: a host filter (or any dep) can
  // change while a request for the *old* deps is still in flight. Without
  // this guard the UI would flicker back to the stale answer the moment it
  // lands, clobbering the already-correct new one.
  it('ignores a stale response that resolves after a newer request was made', async () => {
    const first = deferred<string[]>()
    const second = deferred<string[]>()
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)

    let dep = 1
    const { result, rerender } = renderHook(() => useApi(fetcher, [dep]))

    dep = 2
    rerender()

    // Resolve the newer request first, then the stale one - the stale
    // result must not overwrite it.
    await act(async () => {
      second.resolve(['fresh'])
      await Promise.resolve()
    })
    await act(async () => {
      first.resolve(['stale'])
      await Promise.resolve()
    })

    expect(result.current.data).toEqual(['fresh'])
  })

  it('does not flip loading back to true or clear good data on a background refresh', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(['good']).mockRejectedValueOnce(new Error('transient'))
    const { result } = renderHook(() => useApi(fetcher, [], 5000))

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.data).toEqual(['good'])

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })

    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(result.current.data).toEqual(['good'])
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('pauses polling while the tab is hidden and catches up immediately when shown again', async () => {
    const fetcher = vi.fn().mockResolvedValue(['x'])
    renderHook(() => useApi(fetcher, [], 5000))

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetcher).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'hidden', { value: true, configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))

    // Time passing while hidden must not trigger a background refresh.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20000)
    })
    expect(fetcher).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'hidden', { value: false, configurable: true })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
    })

    // Becoming visible refreshes immediately, without waiting for the next tick.
    expect(fetcher).toHaveBeenCalledTimes(2)
  })
})
