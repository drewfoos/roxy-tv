// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'

// prefs keeps a cached copy: each case loads a fresh module over its own localStorage.
const load = async () => {
  vi.resetModules()
  return (await import('./prefs')).prefs.get()
}

describe('prefs: the Skip button default went from 15 to 20 seconds', () => {
  beforeEach(() => localStorage.clear())
  it('is 20 s with nothing saved', async () => {
    expect((await load()).skipWindowSec).toBe(20)
  })
  it('moves a 15 saved before the change (the old default, saved with any setting) to 20', async () => {
    localStorage.setItem('roxy.prefs', JSON.stringify({ skipWindowSec: 15, autoplay: false }))
    localStorage.setItem('roxy.prefs.v', '2')
    const p = await load()
    expect(p.skipWindowSec).toBe(20)
    expect(p.autoplay).toBe(false)
    expect(JSON.parse(localStorage.getItem('roxy.prefs')!).skipWindowSec).toBe(20)
  })
  it('keeps any other choice, and a 15 chosen after the change', async () => {
    localStorage.setItem('roxy.prefs', JSON.stringify({ skipWindowSec: 10 }))
    localStorage.setItem('roxy.prefs.v', '2')
    expect((await load()).skipWindowSec).toBe(10)
    localStorage.setItem('roxy.prefs', JSON.stringify({ skipWindowSec: 15 }))
    localStorage.setItem('roxy.prefs.v', '3')
    expect((await load()).skipWindowSec).toBe(15)
  })
  it('still applies the older full-subtitles change to a v1 save', async () => {
    localStorage.setItem('roxy.prefs', JSON.stringify({ subMode: 'signs' }))
    const p = await load()
    expect(p.subMode).toBe('full')
    expect(localStorage.getItem('roxy.prefs.v')).toBe('3')
  })
})

// The page says "Preferences for <profile>", but there was one set for the browser:
// whatever one profile chose, every other profile on this browser got.
describe('prefs: each profile has its own', () => {
  const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p1', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
  const start = async (profileId: string | null = 'p1') => {
    localStorage.setItem('roxy.session', JSON.stringify({ ...S, profileId }))
    vi.resetModules()
    const { prefs } = await import('./prefs')
    const { session } = await import('./session')
    return { prefs, session }
  }
  const stored = (key: string) => JSON.parse(localStorage.getItem(key) ?? 'null')
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('roxy.prefs.v', '3')
  })

  it('keeps what one profile chooses away from the next', async () => {
    const { prefs, session } = await start('p1')
    prefs.set({ subMode: 'none', audioLang: 'jpn' })
    session.update({ profileId: 'p2', profileName: 'Coffee' })
    expect(prefs.get().subMode).toBe('full')
    expect(prefs.get().audioLang).toBe('eng')
    prefs.set({ quality: 720 })
    session.update({ profileId: 'p1', profileName: 'Drew' })
    expect(prefs.get()).toMatchObject({ subMode: 'none', audioLang: 'jpn', quality: 'original' })
  })

  it('survives a reload', async () => {
    const first = await start('p1')
    first.prefs.set({ autoSkip: true })
    expect((await start('p1')).prefs.get().autoSkip).toBe(true)
    expect((await start('p2')).prefs.get().autoSkip).toBe(false)
  })

  it('starts a profile from the settings this browser had, and leaves those for the others', async () => {
    const before = { audioLang: 'jpn', autoplay: false, skipWindowSec: 10 }
    localStorage.setItem('roxy.prefs', JSON.stringify(before))
    const { prefs, session } = await start('p1')
    expect(prefs.get()).toMatchObject(before)
    prefs.set({ autoplay: true })
    expect(stored('roxy.prefs')).toEqual(before) // not touched by a profile's change
    session.update({ profileId: 'p2' })
    expect(prefs.get()).toMatchObject(before) // the other profile still has what it had
  })

  it('tells listeners when the profile changes (the page shows the other set)', async () => {
    const { prefs, session } = await start('p1')
    prefs.set({ subMode: 'none' })
    let told = 0
    const off = prefs.subscribe(() => told++)
    const p1 = prefs.get()
    expect(prefs.get()).toBe(p1) // the same object until something changes
    session.update({ profileId: 'p2' })
    expect(told).toBe(1)
    expect(prefs.get().subMode).toBe('full')
    off()
  })

  it('uses the browser-wide set with no profile, or signed out', async () => {
    const { prefs, session } = await start(null)
    prefs.set({ quality: 480 })
    expect(stored('roxy.prefs').quality).toBe(480)
    session.set(null)
    expect(prefs.get().quality).toBe(480)
  })

  it('still moves an old default in the settings a profile starts from', async () => {
    localStorage.setItem('roxy.prefs', JSON.stringify({ skipWindowSec: 15, autoplay: false }))
    localStorage.setItem('roxy.prefs.v', '2')
    const { prefs } = await start('p1')
    expect(prefs.get()).toMatchObject({ skipWindowSec: 20, autoplay: false })
  })
})
