import { describe, expect, it } from 'vitest'
import { entryCompleteness, entryRows, rowNumber, plainEntries, sectionEntries } from './sections'
import type { Item, Section } from './types'

const season = (id: string, n: number) => ({ id, type: 'season', title: n === 0 ? 'Specials' : `Season ${n}`, indexAired: n }) as Item
const s0 = season('s0', 0)
const s1 = season('s1', 1)
const s2 = season('s2', 2)
const film = { id: 'film', type: 'movie', title: 'The Movie', year: 2014 } as Item

describe('sectionEntries', () => {
  it('lists seasons, specials between them and films in release order', () => {
    const sections: Section[] = [
      { kind: 'season', title: 'Season 1', seasonId: 's1', episodes: 12 },
      { kind: 'specials', title: 'OVAs', seasonId: 's0', episodeIds: ['ova1', 'ova2'], episodes: 2, year: 2012 },
      { kind: 'season', title: 'Season 2', seasonId: 's2', episodes: 12 },
      { kind: 'movie', title: 'The Movie', item: film, episodes: 1, year: 2014 },
      { kind: 'specials', title: 'Specials', seasonId: 's0', episodeIds: ['odd'], episodes: 1 },
    ]
    const got = sectionEntries(sections, [s0, s1, s2])!
    expect(got.map((e) => e.title)).toEqual(['Season 1', 'OVAs', 'Season 2', 'The Movie', 'Specials'])
    expect(got[1]).toMatchObject({ seasonId: 's0', ids: ['ova1', 'ova2'], count: 2 })
    expect(got[3]).toMatchObject({ movie: film, label: 'Film · 2014' })
  })
  // Keys by what the entry holds: a new OVA coming in before an entry does not make it
  // another entry (the page keeps its pick by key).
  it('keeps entry keys when an entry is added before them', () => {
    const film1: Section = { kind: 'movie', title: 'The Movie', item: film, episodes: 1, year: 2014 }
    const ova: Section = { kind: 'specials', title: 'OVAs', seasonId: 's0', episodeIds: ['ova1'], episodes: 1 }
    const before = sectionEntries([{ kind: 'season', title: 'Season 1', seasonId: 's1', episodes: 12 }, film1], [s0, s1])!
    const after = sectionEntries([{ kind: 'season', title: 'Season 1', seasonId: 's1', episodes: 12 }, ova, film1], [s0, s1])!
    expect(after.find((e) => e.movie)!.key).toBe(before.find((e) => e.movie)!.key)
    expect(new Set(after.map((e) => e.key)).size).toBe(after.length)
  })
  it('falls back to the plain list when the sections cannot be used', () => {
    expect(sectionEntries(null, [s1])).toBeNull()
    expect(sectionEntries([], [s1])).toBeNull()
    expect(sectionEntries([{ kind: 'season', title: 'Gone', seasonId: 'gone', episodes: 1 }], [s1])).toBeNull()
    expect(plainEntries([s0, s2, s1]).map((e) => e.title)).toEqual(['Season 1', 'Season 2', 'Specials'])
  })
})

describe('entryRows', () => {
  it('keeps a group of specials to its own episodes, in its order', () => {
    const eps = ['odd', 'ova2', 'ova1'].map((id) => ({ id, type: 'episode', title: id }) as Item)
    expect(entryRows({ key: 'x', title: 'OVAs', seasonId: 's0', ids: ['ova1', 'ova2'] }, eps).map((e) => e.id)).toEqual(['ova1', 'ova2'])
    expect(entryRows({ key: 's1', title: 'Season 1', seasonId: 's1' }, eps)).toHaveLength(3)
  })
})

describe('rowNumber', () => {
  const ep = (id: string, n: number) => ({ id, type: 'episode', title: id, indexAired: n }) as Item
  it('numbers a group of specials by place, not by the file numbers (KonoSuba 3..7)', () => {
    const rows = [ep('choker', 3), ep('art', 4)]
    const entry = { key: 'x', title: 'Specials', seasonId: 's0', ids: ['choker', 'art'] }
    expect(rows.map((r) => rowNumber(entry, rows, r))).toEqual([1, 2])
  })
  it('keeps a season episode its own number and gives a film none', () => {
    expect(rowNumber({ key: 's1', title: 'Season 1', seasonId: 's1' }, [], ep('e5', 5))).toBe(5)
    expect(rowNumber({ key: 'f', title: 'Legend of Crimson', movie: film }, [], ep('crimson', 5))).toBeNull()
  })
})

describe('entryCompleteness', () => {
  const e = (id: string) => ({ id, type: 'episode', title: id }) as Item
  it('a group of specials missing one it names is incomplete, of its own size', () => {
    const entry = { key: 'x', title: 'Specials', seasonId: 's0', ids: ['a', 'b', 'c'] }
    const rows = entryRows(entry, [e('a'), e('c'), e('z')])
    expect(entryCompleteness(entry, rows, { complete: true, total: 3 })).toEqual({ complete: false, expected: 3 })
  })
  it('every named special, from a whole read: complete', () => {
    const entry = { key: 'x', title: 'OVAs', seasonId: 's0', ids: ['a'] }
    expect(entryCompleteness(entry, entryRows(entry, [e('a')]), { complete: true }).complete).toBe(true)
  })
  it('a season from a read cut short: incomplete, of the server total', () => {
    expect(entryCompleteness({ key: 's1', title: 'Season 1', seasonId: 's1' }, [e('a'), e('b')], { complete: false, total: 5 })).toEqual({ complete: false, expected: 5 })
  })
  it('a whole season: complete', () => {
    expect(entryCompleteness({ key: 's1', title: 'Season 1', seasonId: 's1' }, [e('a')], { complete: true, total: 1 }).complete).toBe(true)
  })
})
