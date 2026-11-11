// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, NetworkError } from './client'
import { connection, connectionWords, PROBE_DELAYS_MS } from './connection'
import { session } from './session'

const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body })
const dead = () => Promise.reject(new TypeError('Failed to fetch'))

function online(v: boolean) {
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => v })
}

describe('a server that cannot be reached', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    session.set(S)
    connection.reset()
    online(true)
  })
  afterEach(() => {
    connection.reset()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    session.set(null)
    online(true)
  })

  it('is told in plain words, never as the browser\'s "Failed to fetch"', async () => {
    vi.stubGlobal('fetch', vi.fn(dead))
    const err = await api.item('x').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(NetworkError)
    expect((err as Error).message).toBe("Can't reach your Roxy server")
    expect(connection.get()).toBe('away')
  })

  it('is "offline" when the browser itself has no network', async () => {
    online(false)
    vi.stubGlobal('fetch', vi.fn(dead))
    const err = await api.item('x').catch((e: unknown) => e)
    expect((err as Error).message).toBe("You're offline")
    expect(connection.get()).toBe('offline')
  })

  it('does not sign the browser out', async () => {
    vi.stubGlobal('fetch', vi.fn(dead))
    await api.item('x').catch(() => undefined)
    expect(session.get()?.token).toBe('t')
  })

  it('is checked again by itself, and says once when it is back', async () => {
    let up = false
    const fetchMock = vi.fn((url: string) => (up ? Promise.resolve(ok(url === '/healthz' ? {} : { id: 'x' })) : dead()))
    vi.stubGlobal('fetch', fetchMock)
    let back = 0
    const off = connection.onBack(() => back++)
    await api.item('x').catch(() => undefined)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(PROBE_DELAYS_MS[0])
    expect(fetchMock).toHaveBeenLastCalledWith('/healthz', expect.anything())
    expect(connection.get()).toBe('away')
    expect(back).toBe(0)

    up = true
    await vi.advanceTimersByTimeAsync(PROBE_DELAYS_MS[1])
    expect(connection.get()).toBe('ok')
    expect(back).toBe(1)

    // Back: the checks stop.
    const calls = fetchMock.mock.calls.length
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchMock.mock.calls.length).toBe(calls)
    off()
  })

  it('counts any answer as reachable, an error status too', async () => {
    vi.stubGlobal('fetch', vi.fn(dead))
    await api.item('x').catch(() => undefined)
    expect(connection.get()).toBe('away')
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 404, json: async () => ({ error: 'no such item' }) })))
    await expect(api.item('x')).rejects.toThrow('no such item')
    expect(connection.get()).toBe('ok')
  })

  it('Try again asks at once', async () => {
    vi.stubGlobal('fetch', vi.fn(dead))
    await api.item('x').catch(() => undefined)
    vi.stubGlobal('fetch', vi.fn(async () => ok({})))
    await expect(connection.check()).resolves.toBe(true)
    expect(connection.get()).toBe('ok')
  })

  it('never speaks of downloads: the web app has none', () => {
    for (const link of ['offline', 'away'] as const) {
      const w = connectionWords(link)
      expect(`${w.strip} ${w.title} ${w.body}`).not.toMatch(/ownload/)
    }
  })
})
