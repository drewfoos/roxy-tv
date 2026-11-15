// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, CALL_LIMIT_MS, callLimitMs, NetworkError, orEmpty, SLOW_CALL_LIMIT_MS } from './client'
import { connection } from './connection'
import { session } from './session'

const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
const refused = { ok: false, status: 401, json: async () => ({ error: 'sign in again' }) }

// What a 401 does to the session. It means the token the request carried is dead, which
// is this browser's session only if that is still the token it holds.
describe('a request the server answers with 401', () => {
  beforeEach(() => {
    localStorage.clear()
    session.set(S)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    session.set(null)
  })

  it('signs this browser out', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => refused))
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toBeNull()
    expect(localStorage.getItem('roxy.session')).toBeNull()
  })

  it('leaves alone a newer session another tab stored while the request was out', async () => {
    const newer = { ...S, token: 't2' }
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        localStorage.setItem('roxy.session', JSON.stringify(newer)) // the other tab signs in again
        return refused
      }),
    )
    let told = 0
    const off = session.subscribe(() => told++)
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    off()
    expect(JSON.parse(localStorage.getItem('roxy.session')!)).toEqual(newer)
    expect(session.get()).toEqual(newer) // and this tab goes on with it
    expect(told).toBe(1)
  })

  it('leaves alone a session this tab started while the request was out', async () => {
    const newer = { ...S, token: 't2' }
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        session.set(newer)
        return refused
      }),
    )
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toEqual(newer)
  })

  it('signs out a session kept for the tab only (storage refuses writes)', async () => {
    localStorage.clear()
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })
    session.set(S)
    spy.mockRestore()
    vi.stubGlobal('fetch', vi.fn(async () => refused))
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toBeNull()
  })
})

// A profile deleted on another device: the server refuses requests naming it (409 with
// code profile_gone). The browser forgets the profile, which shows "Who's watching?".
describe('a request the server refuses because its profile is gone', () => {
  const gone = { ok: false, status: 409, json: async () => ({ error: 'That profile no longer exists.', code: 'profile_gone' }) }
  beforeEach(() => {
    localStorage.clear()
    session.set(S)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    session.set(null)
  })

  it('forgets the profile and stays signed in', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => gone))
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toEqual({ ...S, profileId: null, profileName: '' })
  })

  it('leaves alone a profile picked while the request was out', async () => {
    const picked = { ...S, profileId: 'p2', profileName: 'Coffee' }
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        session.set(picked)
        return gone
      }),
    )
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toEqual(picked)
  })

  it('keeps the profile on any other conflict', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 409, json: async () => ({ error: 'already requested' }) })))
    await expect(api.item('x')).rejects.toBeInstanceOf(ApiError)
    expect(session.get()).toEqual(S)
  })
})

// A side load a page can do without used to be remembered as empty whatever went wrong,
// so a row lost to a dropped connection stayed away after the server was back.
describe('orEmpty', () => {
  it('gives the empty value for an error the server answered with', () => {
    expect(orEmpty({ items: [] })(new ApiError(404, 'not found'))).toEqual({ items: [] })
  })
  it('throws on when there was no answer at all', () => {
    expect(() => orEmpty({ items: [] })(new NetworkError("Can't reach your Roxy server"))).toThrow(NetworkError)
  })
})

// A server that takes the connection and then says nothing. A call had no limit and
// waited as long as the browser did (minutes), the page on its skeleton all the while.
describe('a call that gets no answer', () => {
  const silent = () =>
    vi.fn(
      (_url: string, init?: RequestInit) =>
        new Promise<never>((_res, rej) => init?.signal?.addEventListener('abort', () => rej(new DOMException('aborted', 'AbortError')))),
    )
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    session.set(S)
    connection.reset()
  })
  afterEach(() => {
    connection.reset()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    session.set(null)
  })

  it('is given up on after its limit, as a server that is away', async () => {
    vi.stubGlobal('fetch', silent())
    let outcome: unknown = 'waiting'
    api.item('x').then(
      () => (outcome = 'answered'),
      (e) => (outcome = e),
    )
    await vi.advanceTimersByTimeAsync(CALL_LIMIT_MS - 1)
    expect(outcome).toBe('waiting')
    await vi.advanceTimersByTimeAsync(1)
    expect(outcome).toBeInstanceOf(NetworkError)
    expect(connection.get()).toBe('away')
  })

  it('also when the answer began and its body never came', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => ({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        text: () => new Promise<string>((_res, rej) => init?.signal?.addEventListener('abort', () => rej(new DOMException('aborted', 'AbortError')))),
      })),
    )
    const call = api.item('x').catch((e) => e)
    await vi.advanceTimersByTimeAsync(CALL_LIMIT_MS)
    expect(await call).toBeInstanceOf(NetworkError)
    expect(connection.get()).toBe('away')
  })

  it('waits longer for the calls that are slow by nature', async () => {
    const fetchMock = silent()
    vi.stubGlobal('fetch', fetchMock)
    let films: unknown = 'waiting'
    api.discoverFilms(1).then(
      () => (films = 'answered'),
      (e) => (films = e),
    )
    let change: unknown = 'waiting'
    api.linkApprove('ABCD').then(
      () => (change = 'answered'),
      (e) => (change = e),
    )
    await vi.advanceTimersByTimeAsync(SLOW_CALL_LIMIT_MS - 1)
    expect(films).toBe('waiting')
    expect(change).toBe('waiting')
    await vi.advanceTimersByTimeAsync(1)
    expect(films).toBeInstanceOf(NetworkError)
    expect(change).toBeInstanceOf(NetworkError)
  })

  it('knows which calls those are', () => {
    expect(callLimitMs('GET', '/v1/items/x')).toBe(CALL_LIMIT_MS)
    expect(callLimitMs('GET', '/v1/home')).toBe(CALL_LIMIT_MS)
    expect(callLimitMs('GET', '/v1/discover/tv/1/films')).toBe(SLOW_CALL_LIMIT_MS)
    expect(callLimitMs('GET', '/v1/link/ABCD/wait?secret=s')).toBe(SLOW_CALL_LIMIT_MS)
    expect(callLimitMs('POST', '/v1/discover/requests')).toBe(SLOW_CALL_LIMIT_MS)
    expect(callLimitMs('PUT', '/v1/settings/arr')).toBe(SLOW_CALL_LIMIT_MS)
    expect(CALL_LIMIT_MS).toBeLessThan(SLOW_CALL_LIMIT_MS)
  })

  it('leaves no timer behind once a call is answered', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, text: async () => '{"id":"x"}' })))
    const before = vi.getTimerCount()
    await api.item('x')
    expect(vi.getTimerCount()).toBe(before)
  })
})

// A call the page that made it no longer waits for (the sign-in code's wait, held open
// by the server for 25 s): leaving ends it, and that says nothing about the server.
describe('a call whose page is gone', () => {
  beforeEach(() => {
    localStorage.clear()
    session.set(null)
    connection.reset()
  })
  afterEach(() => {
    connection.reset()
    vi.unstubAllGlobals()
  })

  it('ends at once, without the server being taken for away', async () => {
    let sent: AbortSignal | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string, init?: RequestInit) => {
        sent = init?.signal ?? undefined
        return new Promise<never>((_res, rej) => init?.signal?.addEventListener('abort', () => rej(new DOMException('aborted', 'AbortError'))))
      }),
    )
    const left = new AbortController()
    const wait = api.linkWait('ABCDEF', 's', left.signal).catch((e) => e)
    left.abort()
    const e = await wait
    expect(sent?.aborted).toBe(true)
    expect(e).not.toBeInstanceOf(NetworkError)
    expect((e as Error).name).toBe('AbortError')
    expect(connection.get()).toBe('ok')
  })
})
