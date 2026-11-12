import type { DiscoverFilm, DiscoverFilms } from '../api/types'

/**
 * The films of an anime series on its Discover page. Everything that is a rule (where a
 * film sits, whether it comes with a request, what can be requested) is the server's and
 * arrives in the data; what is here only arranges and words it.
 */

export interface PlacedFilms {
  /** Before the first season. */
  before: DiscoverFilm[]
  /** After the season with that number. */
  after: Map<number, DiscoverFilm[]>
  /** Apart from the seasons: "More films". */
  more: DiscoverFilm[]
}

/** Films by the place the server gave them; one whose season this page does not list goes under "More films". */
export function placeFilms(films: DiscoverFilm[] | null | undefined, seasonNumbers: number[]): PlacedFilms {
  const out: PlacedFilms = { before: [], after: new Map(), more: [] }
  for (const f of films ?? []) {
    if (f.afterSeason === 0) out.before.push(f)
    else if (f.afterSeason > 0 && seasonNumbers.includes(f.afterSeason)) out.after.set(f.afterSeason, [...(out.after.get(f.afterSeason) ?? []), f])
    else out.more.push(f)
  }
  return out
}

/** The TMDB ids the main Request button sends: exactly the films the server marked for it. */
export function buttonFilms(films: DiscoverFilms | null | undefined): number[] {
  return (films?.films ?? []).filter((f) => f.withRequest && !!f.tmdbId).map((f) => f.tmdbId as number)
}

/** " + 3 films" for the main button's label; nothing without films. */
export function filmsSuffix(n: number): string {
  return n <= 0 ? '' : n === 1 ? ' + 1 film' : ` + ${n} films`
}

export type FilmState = 'library' | 'requested' | 'approval' | 'failed' | 'radarr' | 'request' | 'none'

/** What a film's row shows on the right. */
export function filmState(f: DiscoverFilm, requestsEnabled: boolean): FilmState {
  if (f.libraryItemId) return 'library'
  if (f.requestStatus === 'approval') return 'approval'
  if (f.requestStatus === 'failed' || f.requestStatus === 'declined') return f.tmdbId && requestsEnabled ? 'request' : 'failed'
  if (f.requestStatus) return 'requested'
  if (f.inRadarr) return 'radarr'
  return f.tmdbId && requestsEnabled ? 'request' : 'none'
}

/** The line under a film's title: "Film · 2019 · Side story". */
export function filmLine(f: DiscoverFilm): string {
  const why = f.withRequest ? 'Comes with the request' : f.note
  return ['Film', f.year || null, why || null].filter(Boolean).join('  ·  ')
}
