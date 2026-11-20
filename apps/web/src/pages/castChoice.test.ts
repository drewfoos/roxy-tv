import { describe, expect, it } from 'vitest'
import type { CastGroup } from '../api/types'
import { shownCast } from './castChoice'

const group = (language: string, name: string, ...people: string[]): CastGroup => ({ language, name, members: people.map((p) => ({ name: p })) })
const tmdb = { cast: [{ name: 'TMDB person' }] }
const casts = [group('jpn', 'Japanese', 'Asami Seto'), group('eng', 'English', 'Erica Mendez')]

describe('shownCast', () => {
  it('shows the cast the server chose, with a switch', () => {
    const s = shownCast({ tmdb, casts, castLanguage: 'eng' })
    expect(s.language).toBe('eng')
    expect(s.members.map((m) => m.name)).toEqual(['Erica Mendez'])
    expect(s.groups.map((g) => g.name)).toEqual(['Japanese', 'English'])
  })
  it("follows the viewer's switch over the server's choice", () => {
    expect(shownCast({ tmdb, casts, castLanguage: 'eng' }, 'jpn').members[0].name).toBe('Asami Seto')
  })
  it('falls back to the first cast when the choice names none of them', () => {
    expect(shownCast({ tmdb, casts, castLanguage: 'deu' }, 'fra').language).toBe('jpn')
    expect(shownCast({ tmdb, casts }).language).toBe('jpn')
  })
  it('keeps the TMDB cast, with no switch, when the server sent no choice of casts', () => {
    for (const d of [{ tmdb }, { tmdb, casts: [casts[0]] }, { tmdb, casts: [casts[0], group('eng', 'English')] }]) {
      const s = shownCast(d)
      expect(s.groups).toEqual([])
      expect(s.members.map((m) => m.name)).toEqual(['TMDB person'])
    }
    expect(shownCast(null).members).toEqual([])
  })
})
