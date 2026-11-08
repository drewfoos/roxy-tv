import { describe, expect, it } from 'vitest'
import { entryRows, plainEntries, sectionEntries } from './sections'
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
