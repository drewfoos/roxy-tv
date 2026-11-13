// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Item } from '../api/types'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// What is not read yet is a placeholder in its final place, never words that change and
// never a line that pushes what is already on the page.

const api = vi.hoisted(() => ({
  item: vi.fn(),
  items: vi.fn(),
  allItems: vi.fn(),
  sections: vi.fn(),
  next: vi.fn(),
  related: vi.fn(),
  similar: vi.fn(),
  extras: vi.fn(),
  imageUrl: vi.fn(() => null),
}))
vi.mock('../api/client', () => ({ api, px: (n: number) => n, ApiError: class extends Error {} }))
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))
vi.mock('../components/Extras', () => ({ ExtrasRow: () => null }))
vi.mock('../components/DiscoverBits', () => ({ DiscoverGrid: () => null }))
vi.mock('../components/Cards', () => ({
  Art: () => null,
  CardMenu: () => null,
  ProgressBar: () => null,
  SaveButton: () => null,
  TitleCard: () => null,
  Row: ({ children }: { children?: unknown }) => <div className="row">{children as never}</div>,
  isWatched: () => false,
  useWatchedState: () => [false, () => undefined],
  withPlayed: (x: unknown) => x,
}))
vi.mock('../components/TitlePage', () => ({
  TitleBanner: ({ actions }: { actions?: unknown }) => <div className="banner">{actions as never}</div>,
  TitleBelow: ({ children }: { children?: unknown }) => <div className="below">{children as never}</div>,
  SectionTabs: () => <div className="tabs" />,
}))

import { clearAsyncCache } from '../hooks/useAsync'
import { Movie } from './Movie'
import { Series } from './Series'

const item = (over: Partial<Item>): Item => ({ id: 'x', type: 'series', title: 'A show', ...over }) as Item

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  clearAsyncCache()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

const open = async (path: string) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/series/:id" element={<Series />} />
          <Route path="/movie/:id" element={<Movie />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  for (let i = 0; i < 4; i++) await act(async () => {})
}
const later = <T,>() => {
  let answer: (v: T) => void = () => undefined
  const promise = new Promise<T>((r) => (answer = r))
  return { promise, answer: async (v: T) => { await act(async () => answer(v)); for (let i = 0; i < 4; i++) await act(async () => {}) } }
}

describe('a series page while its seasons are being read', () => {
  it('draws lines for the season and the Play words, then the real ones', async () => {
    const seasons = later<{ items: Item[] }>()
    api.item.mockResolvedValue(item({ id: 's', title: 'A show' }))
    api.items.mockReturnValue(seasons.promise)
    api.allItems.mockResolvedValue({ items: [item({ id: 'e1', type: 'episode', title: 'One', seasonNumber: 1, episodeNumber: 1, availability: 'available' } as Partial<Item>)] })
    api.sections.mockRejectedValue(new Error('older server'))
    api.next.mockResolvedValue(item({ id: 'e1', type: 'episode', title: 'One', seasonNumber: 1, episodeNumber: 1 } as Partial<Item>))
    await open('/series/s')

    const play = () => host.querySelector('.banner button') as HTMLButtonElement
    expect(play().textContent?.trim()).toBe('')
    expect(play().querySelector('[data-pending="play"]')).toBeTruthy()
    expect(host.querySelector('.seasonrow [data-pending="season"]')).toBeTruthy()
    expect(host.querySelector('.seasonrow')?.textContent).not.toContain('A show')

    await seasons.answer({ items: [item({ id: 'se1', type: 'season', title: 'Season 1', seasonNumber: 1 } as Partial<Item>)] })
    expect(host.querySelector('[data-pending]')).toBeNull()
    expect(play().textContent).toContain('Start watching')
    expect(host.querySelector('.seasonrow')?.textContent).toContain('Season 1')
  })
})

describe("a film's \"Part of\" line", () => {
  it('arrives under the film, not above it', async () => {
    const related = later<{ related: unknown[]; partOf: Item[] }>()
    api.item.mockResolvedValue(item({ id: 'm', type: 'movie', title: 'A film', availability: 'available' } as Partial<Item>))
    api.related.mockReturnValue(related.promise)
    await open('/movie/m')
    const film = host.querySelector('.grid--episodes') as HTMLElement
    expect(film).toBeTruthy()

    await related.answer({ related: [], partOf: [item({ id: 's', title: 'Its series' })] })
    const line = [...host.querySelectorAll('a')].find((a) => a.textContent === 'Its series') as HTMLElement
    expect(line).toBeTruthy()
    expect(host.querySelector('.below')?.contains(line)).toBe(false)
    expect(film.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})
