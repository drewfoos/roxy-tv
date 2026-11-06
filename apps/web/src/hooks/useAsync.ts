import { useCallback, useEffect, useRef, useState } from 'react'
import { session } from '../api/session'

export interface AsyncState<T> {
  data: T | null
  error: string | null
  loading: boolean
  reload: (silent?: boolean) => void
}

/**
 * One request per key, with a reload that keeps the old data on screen (the way the
 * phone refreshes after marking something watched). Results are kept per key so going
 * back to a page paints at once: the most recently used CACHE_MAX keys, since every
 * title opened adds several (the item, its seasons, its episodes with file details,
 * related titles) and a long session used to keep them all.
 */
const CACHE_MAX = 120
const cache = new Map<string, unknown>() // insertion order = least recently used first
const fetchedAt = new Map<string, number>()

/**
 * The cache belongs to one login and profile. `epoch` counts clears: a request records it
 * when it starts and its answer is dropped if the cache was cleared meanwhile, so a page
 * for the profile just left cannot write its Continue Watching or Watchlist back under a
 * key the next profile reads.
 */
let epoch = 0
const identity = () => {
  const s = session.get()
  return s ? `${s.userId}|${s.token}|${s.profileId ?? ''}` : ''
}
let lastIdentity = identity()
// Signing in or out (including a 401), or switching profile, clears the cache by itself;
// sign-out used to leave the last account's pages for whoever signed in next.
session.subscribe(() => {
  const id = identity()
  if (id !== lastIdentity) {
    lastIdentity = id
    clearAsyncCache()
  }
})

function cacheGet(key: string): unknown {
  const v = cache.get(key)
  cache.delete(key) // move to the most recent end
  cache.set(key, v)
  return v
}

function cachePut(key: string, value: unknown) {
  cache.delete(key)
  cache.set(key, value)
  fetchedAt.set(key, Date.now())
  // A page on screen keeps its own copy in state; dropping its entry only means the next
  // visit asks the server first.
  for (const old of cache.keys()) {
    if (cache.size <= CACHE_MAX) break
    cache.delete(old)
    fetchedAt.delete(old)
  }
}

/**
 * `freshMs`: a cached result younger than this is shown without asking again (Discover's
 * TMDB rows are ~100 KB and change slowly); 0 = always refresh in the background.
 */
/** What a component last loaded, and for which key and cache generation (see useAsync). */
interface Held<T> {
  key: string | null
  epoch: number
  data: T | null
  error: string | null
  loading: boolean
}

export function useAsync<T>(key: string | null, fn: () => Promise<T>, deps: unknown[] = [], freshMs = 0): AsyncState<T> {
  // The result is held together with the key and cache generation it belongs to, and only
  // handed out while both still match. A plain data state kept the previous key's result
  // for the renders between a key change and the effect below (Episode A's item while the
  // URL already said B), and a component that stayed on screen through a profile switch
  // kept the old profile's data the same way.
  const [held, setHeld] = useState<Held<T>>(() => ({
    key,
    epoch,
    data: key && cache.has(key) ? (cacheGet(key) as T) : null,
    error: null,
    loading: key != null && !cache.has(key),
  }))
  const fnRef = useRef(fn)
  fnRef.current = fn
  const gen = useRef(0)
  const again = useRef<(silent: boolean) => void>(() => undefined)

  const run = useCallback(
    (silent = false) => {
      if (key == null) return
      const my = ++gen.current
      const myEpoch = epoch
      if (!silent && !cache.has(key)) {
        setHeld((h) => (h.key === key && h.epoch === myEpoch ? { ...h, loading: true } : { key, epoch: myEpoch, data: null, error: null, loading: true }))
      }
      fnRef
        .current()
        .then((d) => {
          if (my !== gen.current) return
          // Asked for the login / profile since left: drop it, and ask again for this one
          // (a component that stays on screen through the switch, like the footer).
          if (myEpoch !== epoch) return again.current(silent)
          cachePut(key, d)
          setHeld({ key, epoch: myEpoch, data: d, error: null, loading: false })
        })
        .catch((e: unknown) => {
          if (my !== gen.current) return
          if (myEpoch !== epoch) return again.current(silent)
          const error = e instanceof Error ? e.message : String(e)
          setHeld((h) => (h.key === key && h.epoch === myEpoch ? { ...h, error, loading: false } : { key, epoch: myEpoch, data: null, error, loading: false }))
        })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, ...deps],
  )

  again.current = run
  // Gone from the screen: whatever is still in flight for this component is ignored.
  useEffect(() => () => { gen.current++ }, [])

  useEffect(() => {
    if (key == null) return
    if (cache.has(key)) {
      setHeld({ key, epoch, data: cacheGet(key) as T, error: null, loading: false })
      if (!(freshMs > 0 && Date.now() - (fetchedAt.get(key) ?? 0) < freshMs)) run(true)
    } else {
      setHeld({ key, epoch, data: null, error: null, loading: true })
      run(false)
    }
  }, [key, run, freshMs])

  if (held.key === key && held.epoch === epoch) return { data: held.data, error: held.error, loading: held.loading, reload: run }
  // Rendering for a new key (or after a profile switch) before the effect has caught up:
  // this key's cached result if there is one (read without reordering the cache), else
  // nothing yet. Never the previous key's.
  const cached = key != null && cache.has(key) ? (cache.get(key) as T) : null
  return { data: cached, error: null, loading: key != null && cached == null, reload: run }
}

/** Forget cached pages (after sign-out or a profile switch). */
export function clearAsyncCache() {
  epoch++
  cache.clear()
  fetchedAt.clear()
}

/** The next page that shows `key` asks the server again (after a request changed it). */
export function markStale(key: string) {
  fetchedAt.delete(key)
}
