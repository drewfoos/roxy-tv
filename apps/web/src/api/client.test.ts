// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './client'
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
