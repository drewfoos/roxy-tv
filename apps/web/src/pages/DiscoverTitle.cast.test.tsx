// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DiscoverDetail, ItemDetails } from '../api/types'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  discoverDetail: vi.fn(),
  discoverFilms: vi.fn(),
  discoverRequest: vi.fn(),
  discoverMoreDetails: vi.fn(),
}))
vi.mock('../api/client', () => ({ api, ApiError: class extends Error {} }))
vi.mock('../api/prefs', () => ({ usePrefs: () => ({ audioLang: 'eng' }) }))
vi.mock('../app/useSession', () => ({ useSession: () => ({ isAdmin: true }) }))
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))
vi.mock('../components/PhoneChrome', () => ({ PhoneTitleHeader: () => null }))
vi.mock('../components/TitlePage', () => ({ TitleBanner: () => null, TitleBelow: () => null }))

import { clearAsyncCache } from '../hooks/useAsync'
import { DiscoverTitle } from './DiscoverTitle'

const film = { tmdbId: 600, type: 'movie', title: 'A film', isAnime: true, requestsEnabled: true } as DiscoverDetail
const details = (over: Partial<ItemDetails> = {}): ItemDetails =>
  ({ item: { title: 'A film' }, tmdb: { cast: [{ name: 'Asami Seto', character: 'Mai (voice)' }] }, ...over }) as ItemDetails
const casts = [
  { language: 'jpn', name: 'Japanese', original: true, members: [{ name: 'Asami Seto', character: 'Mai' }] },
  { language: 'eng', name: 'English', members: [{ name: 'Erica Mendez', character: 'Mai' }] },
  { language: 'fra', name: 'French', members: [{ name: 'Un Deux', character: 'Mai' }] },
]

;(globalThis as { ResizeObserver?: unknown }).ResizeObserver ??= class {
  observe() {}
  disconnect() {}
}

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  clearAsyncCache()
  api.discoverDetail.mockResolvedValue(film)
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
      <MemoryRouter initialEntries={['/discover/movie/600']}>
        <Routes>
          <Route path="/discover/:type/:tmdbId" element={<DiscoverTitle />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  await act(async () => {})
}
const names = () => Array.from(host.querySelectorAll('.dcast .card__name')).map((e) => e.textContent)
const list = () => host.querySelector<HTMLSelectElement>('.castlang select')
const pick = (v: string) =>
  act(async () => {
    const el = list()!
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(el, v)
    el.dispatchEvent(new Event('change', { bubbles: true }))
  })

describe("a film's cast on its Discover page", () => {
  it("opens on the cast the server chose for the viewer's language and switches", async () => {
    api.discoverMoreDetails.mockResolvedValue(details({ casts, castLanguage: 'eng' }))
    await open()
    expect(api.discoverMoreDetails).toHaveBeenCalledWith('movie', '600', 'eng')
    expect(names()).toEqual(['Erica Mendez'])
    // One list, naming the cast shown and offering every language the title has.
    expect(list()!.value).toBe('eng')
    expect(Array.from(list()!.options).map((o) => o.textContent)).toEqual(['Japanese (original)', 'English', 'French'])
    await pick('fra')
    expect(names()).toEqual(['Un Deux'])
    expect(list()!.value).toBe('fra')
    await pick('jpn')
    expect(names()).toEqual(['Asami Seto'])
  })

  it('shows the TMDB cast and no switch when the server has one cast only', async () => {
    api.discoverMoreDetails.mockResolvedValue(details())
    await open()
    expect(names()).toEqual(['Asami Seto'])
    expect(list()).toBeNull()
  })
})
