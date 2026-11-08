// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, collectPages, type ItemsQuery } from './client'
import type { Item } from './types'

const ep = (n: number) => ({ id: `e${n}`, type: 'episode', title: `E${n}` }) as unknown as Item
const many = (n: number, from = 1) => Array.from({ length: n }, (_, i) => ep(from + i))

// A fake /v1/items like the server's: 100 without a limit, at most 2000 a request.
const serve = (all: Item[]) =>
  vi.spyOn(api, 'items').mockImplementation(async (q: ItemsQuery) => {
    const limit = Math.min(q.limit && q.limit > 0 ? q.limit : 100, 2000)
    const offset = q.offset ?? 0
    return { items: all.slice(offset, offset + limit), total: all.length }
  })

describe('allItems', () => {
  afterEach(() => vi.restoreAllMocks())
  it('reads every page (4500 items: three requests of 2000)', async () => {
    const spy = serve(many(4500))
    const r = await api.allItems({ type: 'episode', parent: 's', recursive: true })
    expect(r.items).toHaveLength(4500)
    expect(r.items.at(-1)?.id).toBe('e4500')
    expect(spy).toHaveBeenCalledTimes(3)
  })
  it('stops after one request when everything fits', async () => {
    const spy = serve(many(2))
    expect((await api.allItems({ type: 'episode', parent: 's' })).items).toHaveLength(2)
    expect(spy).toHaveBeenCalledTimes(1)
  })
})

// The list changes between two pages (a scan adds or removes episodes): offsets shift, so
// one item would come twice or be skipped. Pages of 3 over a list of 7 here.
describe('collectPages while the list changes', () => {
  // A list that changes once, right after the first page is read.
  const changing = (before: Item[], after: Item[]) => {
    let list = before
    let calls = 0
    return async (limit: number, offset: number) => {
      const r = { items: list.slice(offset, offset + limit), total: list.length }
      if (++calls === 1) list = after
      return r
    }
  }
  const ids = (r: { items: Item[] }) => r.items.map((i) => i.id).join(' ')
  it('one removed before the boundary: read again, none skipped', async () => {
    const before = many(7)
    const after = before.filter((i) => i.id !== 'e2')
    expect(ids(await collectPages(changing(before, after), 3))).toBe('e1 e3 e4 e5 e6 e7')
  })
  it('one added before the boundary: read again, none twice', async () => {
    const before = many(7)
    const after = [ep(0), ...before]
    expect(ids(await collectPages(changing(before, after), 3))).toBe('e0 e1 e2 e3 e4 e5 e6 e7')
  })
  it('a list that never settles: still no item twice', async () => {
    let n = 7
    const r = await collectPages(async (limit, offset) => {
      const list = many(n++)
      return { items: list.slice(offset, offset + limit), total: list.length }
    }, 3)
    expect(new Set(r.items.map((i) => i.id)).size).toBe(r.items.length)
  })
})
