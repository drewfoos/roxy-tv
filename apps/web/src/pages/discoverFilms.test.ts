import { describe, expect, it } from 'vitest'
import type { DiscoverFilm } from '../api/types'
import { buttonFilms, filmLine, filmState, filmsSuffix, placeFilms, filmsPending, requestWords } from './discoverFilms'

const film = (over: Partial<DiscoverFilm>): DiscoverFilm => ({ anilistId: 1, title: 'A film', afterSeason: -1, eligible: false, withRequest: false, ...over })

describe('placeFilms', () => {
  it('puts each film where the server said', () => {
    const p = placeFilms(
      [film({ anilistId: 1, afterSeason: 0 }), film({ anilistId: 2, afterSeason: 1 }), film({ anilistId: 3, afterSeason: 1 }), film({ anilistId: 4, afterSeason: -1 })],
      [1, 2],
    )
    expect(p.before.map((f) => f.anilistId)).toEqual([1])
    expect(p.after.get(1)?.map((f) => f.anilistId)).toEqual([2, 3])
    expect(p.after.get(2)).toBeUndefined()
    expect(p.more.map((f) => f.anilistId)).toEqual([4])
  })
  it('keeps a film whose season the page does not list', () => {
    const p = placeFilms([film({ afterSeason: 7 })], [1, 2])
    expect(p.more).toHaveLength(1)
  })
  it('is empty without films', () => {
    expect(placeFilms(undefined, [1]).more).toEqual([])
  })
})

describe('buttonFilms', () => {
  it('sends only the films the server marked, never its own pick', () => {
    const films = [
      film({ tmdbId: 10, eligible: true, withRequest: true }),
      film({ tmdbId: 11, eligible: true, withRequest: false }), // eligible, but not for this button (already had, or not allowed)
      film({ tmdbId: 12, note: 'Side story' }),
      film({ withRequest: true }), // no TMDB film: nothing to send
    ]
    expect(buttonFilms({ films, auto: false, requestsEnabled: true })).toEqual([10])
    expect(buttonFilms(null)).toEqual([])
  })
})

describe('words', () => {
  it('names the films on the button', () => {
    expect(filmsSuffix(0)).toBe('')
    expect(filmsSuffix(1)).toBe(' + 1 film')
    expect(filmsSuffix(3)).toBe(' + 3 films')
  })
  it('says why a film is only listed, or that it comes along', () => {
    expect(filmLine(film({ year: 2019, note: 'Side story' }))).toBe('Film  ·  2019  ·  Side story')
    expect(filmLine(film({ year: 2019, withRequest: true, note: 'x' }))).toBe('Film  ·  2019  ·  Comes with the request')
    expect(filmLine(film({}))).toBe('Film')
  })
})

describe('filmState', () => {
  it('reads the state from the server fields', () => {
    expect(filmState(film({ tmdbId: 1, libraryItemId: 'i', requestStatus: 'sent' }), true)).toBe('library')
    expect(filmState(film({ tmdbId: 1, requestStatus: 'approval' }), true)).toBe('approval')
    expect(filmState(film({ tmdbId: 1, requestStatus: 'sent' }), true)).toBe('requested')
    expect(filmState(film({ tmdbId: 1, inRadarr: true }), true)).toBe('radarr')
    expect(filmState(film({ tmdbId: 1 }), true)).toBe('request')
  })
  it('offers no request without a TMDB film or with requests off', () => {
    expect(filmState(film({}), true)).toBe('none')
    expect(filmState(film({ tmdbId: 1 }), false)).toBe('none')
    expect(filmState(film({ tmdbId: 1, requestStatus: 'failed' }), false)).toBe('failed')
    expect(filmState(film({ tmdbId: 1, requestStatus: 'failed' }), true)).toBe('request')
  })
})

describe('while the films are being looked for', () => {
  it('waits only for a series that can have films, and only until they answer', () => {
    expect(filmsPending(true, false)).toBe(true)
    expect(filmsPending(true, true)).toBe(false)
    expect(filmsPending(false, false)).toBe(false)
  })
  it('gives a Request button no words until then, so they never change', () => {
    expect(requestWords('Request', 0, true)).toBeNull()
    expect(requestWords('Request', 0, false)).toBe('Request')
    expect(requestWords('Request', 2, false)).toBe('Request + 2 films')
    expect(requestWords('Request 2 seasons', 1, false)).toBe('Request 2 seasons + 1 film')
  })
})
