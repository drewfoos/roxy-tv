// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { HomeRow, Item } from '../api/types'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// Home is read again whenever the tab gets focus. Each read looked every slide of the
// carousel up again (the series and its next episode: two calls a slide), though the
// slides were the same ones nearly every time.

const api = vi.hoisted(() => ({ home: vi.fn(), item: vi.fn(), next: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))
vi.mock('../components/Hero', () => ({ Hero: ({ heroes }: { heroes: { title: Item; play: Item }[] }) => <div className="hero">{heroes.map((h) => `${h.title.id}>${h.play.id}`).join(' ')}</div> }))
vi.mock('../components/Cards', () => ({
  ContinueTile: () => null,
  ThumbTile: () => null,
  TitleCard: () => null,
  Row: ({ title }: { title: string }) => <div className="row">{title}</div>,
}))

import { clearAsyncCache } from '../hooks/useAsync'
import { Home, heroPicksKey } from './Home'

const ep = (id: string, seriesId: string): Item => ({ id, type: 'episode', title: id, seriesId }) as Item
const show = (id: string): Item => ({ id, type: 'series', title: id }) as Item
const rows = (cont: Item[], recent: Item[]): { rows: HomeRow[] } => ({
  rows: [
    { key: 'continue', title: 'Continue', items: cont },
    { key: 'recentEpisodes', title: 'New', items: recent },
  ] as HomeRow[],
})

let host: HTMLDivElement
let root: Root
const settle = () => act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve() })
const focus = async () => {
  await act(async () => { window.dispatchEvent(new Event('focus')) })
  await settle()
}

beforeEach(async () => {
  clearAsyncCache()
  for (const f of Object.values(api)) f.mockReset()
  api.item.mockImplementation(async (id: string) => show(id))
  api.next.mockImplementation(async (id: string) => ep(`${id}-next`, id))
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

describe("Home's carousel when the tab gets focus", () => {
  it('reads Home again but not the same slides', async () => {
    api.home.mockImplementation(async () => rows([ep('a3', 'a')], [ep('b9', 'b')]))
    await act(async () => { root.render(<MemoryRouter><Home /></MemoryRouter>) })
    await settle()
    expect(host.querySelector('.hero')?.textContent).toBe('a>a3 b>b-next')
    const lookups = api.item.mock.calls.length + api.next.mock.calls.length
    expect(lookups).toBe(3)

    await focus()
    expect(api.home).toHaveBeenCalledTimes(2)
    expect(api.item.mock.calls.length + api.next.mock.calls.length).toBe(lookups)
    expect(host.querySelector('.hero')?.textContent).toBe('a>a3 b>b-next')
  })

  it('looks the slides up again when what was watched elsewhere changed them', async () => {
    api.home.mockImplementationOnce(async () => rows([ep('a3', 'a')], [ep('b9', 'b')]))
    api.home.mockImplementation(async () => rows([ep('a4', 'a')], [ep('b9', 'b')]))
    await act(async () => { root.render(<MemoryRouter><Home /></MemoryRouter>) })
    await settle()
    await focus()
    expect(host.querySelector('.hero')?.textContent).toBe('a>a4 b>b-next')
  })

  it('tells the slides apart by what they play and why', () => {
    const a = rows([ep('a3', 'a')], [ep('b9', 'b')]).rows
    expect(heroPicksKey(a)).toBe(heroPicksKey(rows([ep('a3', 'a')], [ep('b9', 'b')]).rows))
    expect(heroPicksKey(a)).not.toBe(heroPicksKey(rows([ep('a4', 'a')], [ep('b9', 'b')]).rows))
    // The same episode, now one being watched: its Play is the episode, not the show's next.
    expect(heroPicksKey(rows([], [ep('b9', 'b')]).rows)).not.toBe(heroPicksKey(rows([ep('b9', 'b')], []).rows))
  })
})
