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
const fetchedAt = new Map<string, number>()

/**
 * `freshMs`: a cached result younger than this is shown without asking again (Discover's
 * TMDB rows are ~100 KB and change slowly); 0 = always refresh in the background.
 */
export function useAsync<T>(key: string | null, fn: () => Promise<T>, deps: unknown[] = [], freshMs = 0): AsyncState<T> {
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
          fetchedAt.set(key, Date.now())
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
      if (!(freshMs > 0 && Date.now() - (fetchedAt.get(key) ?? 0) < freshMs)) run(true)
    } else {
      setData(null)
      run(false)
    }
  }, [key, run, freshMs])

  return { data, error, loading, reload: run }
}

/** Forget cached pages (after sign-out or a profile switch). */
export function clearAsyncCache() {
  cache.clear()
  fetchedAt.clear()
}

/** The next page that shows `key` asks the server again (after a request changed it). */
export function markStale(key: string) {
  fetchedAt.delete(key)
}
