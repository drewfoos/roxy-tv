import { useCallback, useEffect, useRef, useState } from 'react'
import { session } from '../api/session'
import { connection } from '../api/connection'

export interface AsyncState<T> {
  data: T | null
  error: string | null
  loading: boolean
  /**
   * True once this component's own request has answered; false while `data` is only the
   * cached copy from an earlier visit. A form that saves every field waits for it, so it
   * never starts from (and saves back) settings older than the server's.
   */
  fresh: boolean
  /** Resolves once this reload has answered (or failed): for a button that must stay busy until the page shows what it did. */
  reload: (silent?: boolean) => Promise<void>
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
  fresh: boolean
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
    fresh: false,
  }))
  const fnRef = useRef(fn)
  fnRef.current = fn
  const gen = useRef(0)
  const again = useRef<(silent: boolean) => Promise<void>>(() => Promise.resolve())
  // The newest load: one that was overtaken ends when this one does, so a caller waiting
  // on `reload()` is not told "done" while the answer that will be shown is still out.
  const latest = useRef<Promise<void> | null>(null)

  const run = useCallback(
    (silent = false): Promise<void> => {
      if (key == null) return Promise.resolve()
      const my = ++gen.current
      const myEpoch = epoch
      if (!silent && !cache.has(key)) {
        setHeld((h) => (h.key === key && h.epoch === myEpoch ? { ...h, loading: true } : { key, epoch: myEpoch, data: null, error: null, loading: true, fresh: false }))
      }
      const overtaken = () => (latest.current === mine ? undefined : (latest.current ?? undefined))
      // A load that throws before it returns a promise is a failed load like any other:
      // thrown from here it would leave the load it overtook dropped, with nothing shown.
      let asked: Promise<T>
      try {
        asked = fnRef.current()
      } catch (e) {
        asked = Promise.reject(e)
      }
      const mine: Promise<void> = asked
        .then((d) => {
          if (my !== gen.current) return overtaken()
          // Asked for the login / profile since left: drop it, and ask again for this one
          // (a component that stays on screen through the switch, like the footer).
          if (myEpoch !== epoch) return again.current(silent)
          cachePut(key, d)
          setHeld({ key, epoch: myEpoch, data: d, error: null, loading: false, fresh: true })
        })
        .catch((e: unknown) => {
          if (my !== gen.current) return overtaken()
          if (myEpoch !== epoch) return again.current(silent)
          const error = e instanceof Error ? e.message : String(e)
          setHeld((h) => (h.key === key && h.epoch === myEpoch ? { ...h, error, loading: false } : { key, epoch: myEpoch, data: null, error, loading: false, fresh: false }))
        })
      latest.current = mine
      return mine
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, ...deps],
  )

  again.current = run
  // The server is back after being unreachable: a page left showing "can't reach" (or
  // old data its refresh could not replace) loads by itself.
  const failed = useRef(false)
  failed.current = held.key === key && held.error != null
  useEffect(() => connection.onBack(() => { if (failed.current) again.current(true) }), [])
  // Gone from the screen: whatever is still in flight for this component is ignored.
  useEffect(() => () => { gen.current++ }, [])
  // The cache was cleared under this component: ask again (see clearAsyncCache).
  useEffect(() => {
    const ask = () => void again.current(false)
    onClear.add(ask)
    return () => {
      onClear.delete(ask)
    }
  }, [])

  useEffect(() => {
    if (key == null) return
    if (cache.has(key)) {
      setHeld({ key, epoch, data: cacheGet(key) as T, error: null, loading: false, fresh: false })
      if (!(freshMs > 0 && Date.now() - (fetchedAt.get(key) ?? 0) < freshMs)) run(true)
    } else {
      setHeld({ key, epoch, data: null, error: null, loading: true, fresh: false })
      run(false)
    }
  }, [key, run, freshMs])

  if (held.key === key && held.epoch === epoch) return { data: held.data, error: held.error, loading: held.loading, fresh: held.fresh, reload: run }
  // Rendering for a new key (or after a profile switch) before the effect has caught up:
  // this key's cached result if there is one (read without reordering the cache), else
  // nothing yet. Never the previous key's.
  const cached = key != null && cache.has(key) ? (cache.get(key) as T) : null
  return { data: cached, error: null, loading: key != null && cached == null, fresh: false, reload: run }
}

/** The components on screen: each asks again when the cache is cleared under it. */
const onClear = new Set<() => void>()

/**
 * Forget cached pages (after sign-out or a profile switch, or a change that shows on many
 * pages, like a profile's name). What is on screen at that moment asks again: its data is
 * from before the clear, so it is no longer handed out, and with nothing asking, a
 * component that stayed (the footer through a profile being saved) showed nothing until
 * the page was left. Not while signed out: the pages are on their way out.
 */
export function clearAsyncCache() {
  epoch++
  cache.clear()
  fetchedAt.clear()
  if (session.get()) [...onClear].forEach((ask) => ask())
}

/** The next page that shows `key` asks the server again (after a request changed it). */
export function markStale(key: string) {
  fetchedAt.delete(key)
}

/** Back from another tab (something was watched there): reload, keeping what is shown. */
export function useReloadOnFocus(...reloads: Array<(silent?: boolean) => Promise<void>>) {
  useEffect(() => {
    const onFocus = () => reloads.forEach((reload) => void reload(true))
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, reloads)
}

/**
 * Reload every `ms` while the tab is shown (0 = not at all). A tick is skipped while the
 * one before is still unanswered: each reload drops the one before it, so on a link
 * slower than the interval no answer was ever shown.
 */
export function usePoll(reload: (silent?: boolean) => Promise<void>, ms: number) {
  useEffect(() => {
    if (!ms) return
    let out = false
    const h = window.setInterval(() => {
      if (document.hidden || out) return
      out = true
      void reload(true).finally(() => {
        out = false
      })
    }, ms)
    return () => window.clearInterval(h)
  }, [ms, reload])
}
