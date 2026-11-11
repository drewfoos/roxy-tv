// Whether the server can be reached from this tab. The page itself came from the server,
// so this only matters for a tab that is already open: the laptop left the Wi-Fi, or the
// server went down. Then every request fails with the browser's bare "Failed to fetch";
// this turns that into one calm state the whole app can show, checks quietly until the
// server answers again, and tells the pages so they load by themselves.

/** ok: answering. offline: the browser has no network. away: network, but no server. */
export type Link = 'ok' | 'offline' | 'away'

export interface ConnectionWords {
  /** The short line on the strip at the bottom of the screen. */
  strip: string
  title: string
  body: string
}

/** What to say for a lost connection. The web app has no saved videos to point at. */
export function connectionWords(link: Exclude<Link, 'ok'>): ConnectionWords {
  if (link === 'offline')
    return {
      strip: 'No internet connection',
      title: "You're offline",
      body: 'Check your Wi-Fi or network connection. Roxy comes back by itself once you are connected.',
    }
  return {
    strip: "Can't reach your Roxy server",
    title: "Can't reach your Roxy server",
    body: "This device is online, but Roxy isn't answering. Check that the server is on and that you are on the same network. This page loads by itself when it's back.",
  }
}

/** Waits between checks while the server is away: quick at first, then every 15 s. */
export const PROBE_DELAYS_MS = [2000, 4000, 8000, 15000]

let link: Link = 'ok'
const listeners = new Set<() => void>()
const backListeners = new Set<() => void>()
let timer: ReturnType<typeof setTimeout> | null = null
let attempt = 0
let probing: Promise<boolean> | null = null

const hasNetwork = () => typeof navigator === 'undefined' || navigator.onLine !== false

function set(next: Link) {
  if (next === link) return
  const was = link
  link = next
  listeners.forEach((l) => l())
  if (next === 'ok' && was !== 'ok') backListeners.forEach((l) => l())
}

function schedule() {
  if (timer != null || link === 'ok') return
  const wait = PROBE_DELAYS_MS[Math.min(attempt, PROBE_DELAYS_MS.length - 1)]
  attempt++
  timer = setTimeout(() => {
    timer = null
    void connection.check()
  }, wait)
}

function stop() {
  if (timer != null) clearTimeout(timer)
  timer = null
  attempt = 0
}

export const connection = {
  get(): Link {
    return link
  },
  subscribe(fn: () => void): () => void {
    listeners.add(fn)
    return () => listeners.delete(fn)
  },
  /** Called once each time the server answers again after being unreachable. */
  onBack(fn: () => void): () => void {
    backListeners.add(fn)
    return () => backListeners.delete(fn)
  },
  /** A request got no answer at all (not an error status: those are answers). */
  failed() {
    set(hasNetwork() ? 'away' : 'offline')
    schedule()
  },
  /** The server answered something, whatever the status. */
  answered() {
    stop()
    set('ok')
  },
  /** Ask the server now (the Try again button, the browser's "online" event, the timer). */
  check(): Promise<boolean> {
    if (probing) return probing
    probing = fetch('/healthz', { cache: 'no-store' })
      .then(
        (r) => r.ok,
        () => false,
      )
      .then((up) => {
        probing = null
        if (up) connection.answered()
        else if (link !== 'ok') {
          set(hasNetwork() ? 'away' : 'offline')
          schedule()
        }
        return up
      })
    return probing
  },
  /** For tests: back to a fresh page. */
  reset() {
    stop()
    probing = null
    link = 'ok'
  },
}

if (typeof window !== 'undefined') {
  // The browser noticing first: say so at once instead of waiting for a request to fail.
  window.addEventListener('offline', () => connection.failed())
  window.addEventListener('online', () => {
    if (link !== 'ok') void connection.check()
  })
}
