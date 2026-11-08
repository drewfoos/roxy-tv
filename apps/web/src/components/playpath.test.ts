// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import type { Item } from '../api/types'
import { playPath } from './Cards'

const ep = (id: string, s: number, n: number, ud: Partial<NonNullable<Item['userData']>> = {}) =>
  ({ id, type: 'episode', title: id, seasonAired: s, indexAired: n, hasMedia: true, userData: { played: false, positionMs: 0, isFavorite: false, playCount: 0, ...ud } }) as unknown as Item

// The hover panels' Play started nothing (it opened the title's page).
describe('playPath', () => {
  afterEach(() => vi.restoreAllMocks())
  it('plays a film', async () => {
    expect(await playPath('f1', 'movie')).toBe('/watch/f1')
  })
  it('plays the episode a show is up to', async () => {
    vi.spyOn(api, 'items').mockResolvedValue({ items: [ep('e1', 1, 1, { played: true }), ep('e2', 1, 2, { positionMs: 5000 }), ep('e3', 1, 3)], total: 3 } as never)
    expect(await playPath('s1', 'series')).toBe('/watch/e2')
  })
  it("opens a show's page when nothing in it plays, or the episodes cannot be read", async () => {
    vi.spyOn(api, 'items').mockResolvedValue({ items: [], total: 0 } as never)
    expect(await playPath('s1', 'series')).toBe('/series/s1')
    vi.spyOn(api, 'items').mockRejectedValue(new Error('offline'))
    expect(await playPath('s1', 'series')).toBe('/series/s1')
  })
})
