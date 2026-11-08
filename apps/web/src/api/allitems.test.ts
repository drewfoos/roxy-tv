// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, type ItemsQuery } from './client'
import type { Item } from './types'
import { playPath } from '../components/Cards'

// A fake /v1/items like the server's: 100 without a limit, at most 2000 a request.
const serve = (all: Item[]) =>
  vi.spyOn(api, 'items').mockImplementation(async (q: ItemsQuery) => {
    const limit = Math.min(q.limit && q.limit > 0 ? q.limit : 100, 2000)
    const offset = q.offset ?? 0
    return { items: all.slice(offset, offset + limit), total: all.length }
  })

const ep = (n: number, played: boolean) =>
  ({ id: `e${n}`, type: 'episode', title: `E${n}`, seasonAired: 1, indexAired: n, hasMedia: true, userData: { played, positionMs: 0, isFavorite: false, playCount: 0 } }) as unknown as Item

describe('allItems', () => {
  afterEach(() => vi.restoreAllMocks())
  it('reads every page (4500 items: three requests of 2000)', async () => {
    const all = Array.from({ length: 4500 }, (_, i) => ep(i + 1, false))
    const spy = serve(all)
    const r = await api.allItems({ type: 'episode', parent: 's', recursive: true })
    expect(r.items).toHaveLength(4500)
    expect(r.items.at(-1)?.id).toBe('e4500')
    expect(spy).toHaveBeenCalledTimes(3)
  })
  it('stops after one request when everything fits', async () => {
    const spy = serve([ep(1, false), ep(2, false)])
    expect((await api.allItems({ type: 'episode', parent: 's' })).items).toHaveLength(2)
    expect(spy).toHaveBeenCalledTimes(1)
  })
})

// A long show (SpongeBob has 716 episodes): the first 150 are watched, so Play must start
// E151, which a single default request (the first 100) never saw.
describe('Play on a long show', () => {
  afterEach(() => vi.restoreAllMocks())
  it('starts the episode after the first page', async () => {
    serve(Array.from({ length: 716 }, (_, i) => ep(i + 1, i < 150)))
    expect(await playPath('s', 'series')).toBe('/watch/e151')
  })
})
