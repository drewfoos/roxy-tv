// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DiscoverDetail, DiscoverFilm, DiscoverFilms } from '../api/types'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  discoverDetail: vi.fn(),
  discoverFilms: vi.fn(),
  discoverRequest: vi.fn(),
  discoverMoreDetails: vi.fn(),
}))
vi.mock('../api/client', () => ({ api, ApiError: class extends Error {} }))
vi.mock('../app/useSession', () => ({ useSession: () => ({ isAdmin: true }) }))
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))
vi.mock('../components/PhoneChrome', () => ({ PhoneTitleHeader: () => null }))
vi.mock('../components/TitlePage', () => ({
  TitleBanner: ({ actions }: { actions?: unknown }) => <div className="banner">{actions as never}</div>,
  TitleBelow: () => null,
}))

import { clearAsyncCache } from '../hooks/useAsync'
import { DiscoverTitle } from './DiscoverTitle'

const season = (number: number) => ({ number, name: `Season ${number}`, episodes: 12, have: 0, requested: false })
const detail = (over: Partial<DiscoverDetail> = {}): DiscoverDetail =>
  ({ tmdbId: 500, type: 'tv', title: 'A show', isAnime: true, requestsEnabled: true, seasons: [season(1), season(2)], ...over }) as DiscoverDetail
const film = (over: Partial<DiscoverFilm>): DiscoverFilm => ({ anilistId: 1, title: 'A film', afterSeason: -1, eligible: false, withRequest: false, ...over })
const films = (list: DiscoverFilm[], auto = false): DiscoverFilms => ({ films: list, auto, requestsEnabled: true })

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  clearAsyncCache()
  api.discoverRequest.mockResolvedValue({})
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

const open = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/discover/tv/500']}>
        <Routes>
          <Route path="/discover/:type/:tmdbId" element={<DiscoverTitle />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  await act(async () => {})
  await act(async () => {})
}
const button = (text: RegExp) => [...host.querySelectorAll('button')].find((b) => text.test(b.textContent ?? '')) as HTMLButtonElement
const rows = () => [...host.querySelectorAll('.dseasons > .dseason, .dseasons > h3')].map((e) => (e.querySelector('b') ?? e).textContent)

describe('films on a Discover series page', () => {
  it('lists them in the season list where the server put them', async () => {
    api.discoverDetail.mockResolvedValue(detail())
    api.discoverFilms.mockResolvedValue(
      films([
        film({ anilistId: 1, tmdbId: 11, title: 'Between', afterSeason: 1, eligible: true, withRequest: true }),
        film({ anilistId: 2, tmdbId: 12, title: 'Prologue', afterSeason: 0, note: 'Before the first season' }),
        film({ anilistId: 3, tmdbId: 13, title: 'Aside', afterSeason: -1, note: 'Side story' }),
      ]),
    )
    await open()
    expect(rows()).toEqual(['Prologue', 'Season 1', 'Between', 'Season 2', 'More films', 'Aside'])
  })

  it('sends the films the server marked with the main request, and no others', async () => {
    api.discoverDetail.mockResolvedValue(detail())
    api.discoverFilms.mockResolvedValue(
      films([
        film({ anilistId: 1, tmdbId: 11, title: 'Between', afterSeason: 1, eligible: true, withRequest: true }),
        film({ anilistId: 3, tmdbId: 13, title: 'Aside', note: 'Side story' }),
      ]),
    )
    await open()
    const b = button(/^Request \+ 1 film$/)
    expect(b).toBeTruthy()
    await act(async () => b.click())
    expect(api.discoverRequest).toHaveBeenCalledWith('tv', 500, undefined, undefined, [11])
  })

  it('requests a listed film only by its own button, as a film', async () => {
    api.discoverDetail.mockResolvedValue(detail())
    api.discoverFilms.mockResolvedValue(films([film({ anilistId: 3, tmdbId: 13, title: 'Aside', note: 'Side story' })]))
    await open()
    expect(button(/^Request$/)).toBeTruthy() // the main button names no film
    const chip = host.querySelector('.dfilm button') as HTMLButtonElement
    await act(async () => chip.click())
    expect(api.discoverRequest).toHaveBeenCalledWith('movie', 13)
  })

  it('offers no request for a film TMDB could not be matched to', async () => {
    api.discoverDetail.mockResolvedValue(detail())
    api.discoverFilms.mockResolvedValue(films([film({ title: 'Unknown', note: 'Not found on TMDB' })]))
    await open()
    expect(host.querySelector('.dfilm')).toBeTruthy()
    expect(host.querySelector('.dfilm button')).toBeNull()
    expect(host.querySelector('.dfilm a')).toBeNull()
  })

  it('does not ask for films of a series that is not anime', async () => {
    api.discoverDetail.mockResolvedValue(detail({ isAnime: false }))
    await open()
    expect(api.discoverFilms).not.toHaveBeenCalled()
    await act(async () => button(/^Request$/).click())
    expect(api.discoverRequest).toHaveBeenCalledWith('tv', 500, undefined, undefined, [])
  })
})
