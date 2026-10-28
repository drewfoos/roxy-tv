import { useCallback, useEffect, useRef, useState } from 'react'

export interface AsyncState<T> {
  data: T | null
  error: string | null
  loading: boolean
  reload: (silent?: boolean) => void
}

/**
 * One request per key, with a reload that keeps the old data on screen (the way the
 * phone refreshes after marking something watched). Results are kept per key for the
 * life of the tab, so going back to a page paints at once.
 */
const cache = new Map<string, unknown>()

export function useAsync<T>(key: string | null, fn: () => Promise<T>, deps: unknown[] = []): AsyncState<T> {
  const [data, setData] = useState<T | null>(() => (key && cache.has(key) ? (cache.get(key) as T) : null))
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState<boolean>(key != null && !cache.has(key))
  const fnRef = useRef(fn)
  fnRef.current = fn
  const gen = useRef(0)

  const run = useCallback(
    (silent = false) => {
      if (key == null) return
      const my = ++gen.current
      if (!silent && !cache.has(key)) setLoading(true)
      fnRef
        .current()
        .then((d) => {
          if (my !== gen.current) return
          cache.set(key, d)
          setData(d)
          setError(null)
          setLoading(false)
        })
        .catch((e: unknown) => {
          if (my !== gen.current) return
          setError(e instanceof Error ? e.message : String(e))
          setLoading(false)
        })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, ...deps],
  )

  useEffect(() => {
    if (key == null) return
    if (cache.has(key)) {
      setData(cache.get(key) as T)
      setLoading(false)
      run(true)
    } else {
      setData(null)
      run(false)
    }
  }, [key, run])

  return { data, error, loading, reload: run }
}

/** Forget cached pages (after sign-out or a profile switch). */
export function clearAsyncCache() {
  cache.clear()
}
