// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import type { Item } from '../api/types'
import { playPath } from './Cards'

// The hover panels' Play started nothing (it opened the title's page); a show's episode is
// now the server's to work out (GET /v1/items/{id}/next), not every episode loaded here.
describe('playPath', () => {
  afterEach(() => vi.restoreAllMocks())
  it('plays a film', async () => {
    expect(await playPath('f1', 'movie')).toBe('/watch/f1')
  })
  it('plays the episode the server says a show is up to, without loading its episodes', async () => {
    const items = vi.spyOn(api, 'items')
    vi.spyOn(api, 'next').mockResolvedValue({ id: 'e151', type: 'episode', title: 'E151' } as Item)
    expect(await playPath('s1', 'series')).toBe('/watch/e151')
    expect(items).not.toHaveBeenCalled()
  })
  it("opens a show's page when nothing in it plays, or the server cannot say", async () => {
    vi.spyOn(api, 'next').mockResolvedValue(null)
    expect(await playPath('s1', 'series')).toBe('/series/s1')
    vi.spyOn(api, 'next').mockRejectedValue(new Error('offline'))
    expect(await playPath('s1', 'series')).toBe('/series/s1')
  })
})
