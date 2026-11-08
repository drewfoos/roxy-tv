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
