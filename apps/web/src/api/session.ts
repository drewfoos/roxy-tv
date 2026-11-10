// The signed-in state, kept in localStorage so a reload stays signed in. One listener
// set so React can re-render when it changes (sign in, profile switch, sign out).

export interface Session {
  token: string
  userId: string
  userName: string
  isAdmin: boolean
  profileId: string | null
  profileName: string
  profileHasAvatar: boolean
  profileAvatarKey: string
}

const KEY = 'roxy.session'
const DEVICE_KEY = 'roxy.deviceId'
const listeners = new Set<() => void>()
let cached: Session | null | undefined

/** What storage holds now: another tab may have written it since this one last looked. */
function stored(): Session | null {
  try {
    const raw = localStorage.getItem(KEY)
    return raw ? (JSON.parse(raw) as Session) : null
  } catch {
    return null
  }
}

function read(): Session | null {
  if (cached === undefined) cached = stored()
  return cached
}

// Another tab signed in, switched profile or signed out: this tab follows, instead of
// going on as who it was when it loaded (the event only fires for other tabs' writes).
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (e) => {
    if (e.key !== null && e.key !== KEY) return // null: all of storage was cleared
    cached = undefined
    listeners.forEach((l) => l())
  })
}

export const session = {
  get(): Session | null {
    return read()
  },
  set(next: Session | null) {
    cached = next
    try {
      if (next) localStorage.setItem(KEY, JSON.stringify(next))
      else localStorage.removeItem(KEY)
    } catch {
      /* private mode: the session lives for the tab only */
    }
    listeners.forEach((l) => l())
  },
  /**
   * The server refused `token` (a 401): signs out if that is still this browser's token.
   * When another tab has signed in again since the request left, the refusal is about the
   * old token, so the newer session stays (it used to be wiped for every tab).
   */
  expire(token: string) {
    const now = stored()
    if (now && now.token !== token) {
      if (cached?.token !== now.token) {
        cached = now
        listeners.forEach((l) => l())
      }
      return
    }
    if (read()?.token === token) session.set(null)
  },
  update(patch: Partial<Session>) {
    const cur = read()
    if (cur) session.set({ ...cur, ...patch })
  },
  /**
   * Given the login's profile ids as the server just listed them, drops the selected
   * profile if it is not among them (deleted on another device). The server quietly
   * serves the default profile for an unknown one, so this browser went on showing the
   * deleted profile's name while reading and writing another's watch state. Dropping it
   * sends the app to "Who's watching?". True when it was dropped.
   */
  forgetMissingProfile(ids: string[]): boolean {
    const cur = read()
    if (!cur?.profileId || ids.includes(cur.profileId)) return false
    session.set({ ...cur, profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
    return true
  },
  subscribe(l: () => void) {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
  /** A stable id for this browser, so the server lists it as one device. */
  deviceId(): string {
    try {
      let id = localStorage.getItem(DEVICE_KEY)
      if (!id) {
        // crypto.randomUUID only exists on HTTPS or localhost; a LAN address (http://192.168.x.x) has getRandomValues.
        const bytes = new Uint8Array(8)
        crypto.getRandomValues(bytes)
        id = 'web-' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
        localStorage.setItem(DEVICE_KEY, id)
      }
      return id
    } catch {
      return 'web-' + Math.random().toString(36).slice(2, 10)
    }
  },
}

/** The browser as the server will list it under Devices. */
export function deviceName(): string {
  const ua = navigator.userAgent
  const browser = /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Firefox\//.test(ua) ? 'Firefox' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  const os = /Windows/.test(ua) ? 'Windows' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : ''
  return [browser, os].filter(Boolean).join(' on ')
}
