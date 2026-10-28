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

function read(): Session | null {
  if (cached !== undefined) return cached
  try {
    const raw = localStorage.getItem(KEY)
    cached = raw ? (JSON.parse(raw) as Session) : null
  } catch {
    cached = null
  }
  return cached
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
  update(patch: Partial<Session>) {
    const cur = read()
    if (cur) session.set({ ...cur, ...patch })
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
