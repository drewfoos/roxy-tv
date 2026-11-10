// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'

const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }

// A fresh module each time: the session caches what it read.
const load = async () => {
  vi.resetModules()
  return (await import('./session')).session
}

beforeEach(() => localStorage.clear())

describe('session', () => {
  it('survives a reload (kept in localStorage)', async () => {
    ;(await load()).set(S)
    expect((await load()).get()).toEqual(S)
  })
  it('tells listeners, and signs out', async () => {
    const session = await load()
    const seen: unknown[] = []
    const off = session.subscribe(() => seen.push(session.get()))
    session.set(S)
    session.update({ profileName: 'Coffee' })
    session.set(null)
    expect(seen).toEqual([S, { ...S, profileName: 'Coffee' }, null])
    off()
    expect(localStorage.getItem('roxy.session')).toBeNull()
  })
  it('treats a damaged stored session as signed out', async () => {
    localStorage.setItem('roxy.session', '{not json')
    expect((await load()).get()).toBeNull()
  })
  // Each tab kept its own copy from when it loaded: after a sign-in, a profile switch or a
  // sign-out in another tab, this one went on as who it had been.
  it('follows what another tab stores', async () => {
    const session = await load()
    session.set(S)
    let told = 0
    session.subscribe(() => told++)
    const other = { ...S, token: 't2', profileId: 'p2', profileName: 'Coffee' }
    const fromOtherTab = (value: string | null) => {
      if (value === null) localStorage.removeItem('roxy.session')
      else localStorage.setItem('roxy.session', value)
      window.dispatchEvent(new StorageEvent('storage', { key: 'roxy.session', newValue: value }))
    }
    fromOtherTab(JSON.stringify(other))
    expect(session.get()).toEqual(other)
    expect(told).toBe(1)
    fromOtherTab(null) // signed out there
    expect(session.get()).toBeNull()
    expect(told).toBe(2)
    window.dispatchEvent(new StorageEvent('storage', { key: 'roxy.volume', newValue: '0.5' })) // not ours
    expect(told).toBe(2)
  })
  it('keeps working when storage refuses writes (private mode)', async () => {
    const session = await load()
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })
    expect(() => session.set(S)).not.toThrow()
    expect(session.get()).toEqual(S) // for this tab
    spy.mockRestore()
  })
})
