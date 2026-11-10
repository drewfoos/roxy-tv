// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Item } from '../api/types'

// The page runs west of UTC here, as it does at home: a bare "2026-10-30" read as UTC
// midnight is the evening of the 29th there. Set before anything makes a Date.
vi.hoisted(() => {
  process.env.TZ = 'America/Chicago'
})
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  item: vi.fn(),
  allItems: vi.fn(async () => ({ items: [] })),
  imageUrl: () => '',
}))
vi.mock('../api/client', () => ({ api, px: (n: number) => n, ApiError: class extends Error {} }))
vi.mock('../player/Player', () => ({ Player: () => null }))
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))

import { Watch } from './Watch'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

async function show(item: Item) {
  api.item.mockResolvedValue(item)
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[`/watch/${item.id}`]}>
        <Routes>
          <Route path="/watch/:id" element={<Watch />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  await act(async () => {})
}
const film = (airDate: string): Item => ({ id: 'f1', type: 'movie', title: 'A film', airDate }) as Item

describe('the watch page', () => {
  it('runs in a timezone west of UTC (or the next test proves nothing)', () => {
    expect(new Date('2026-10-30').getDate()).toBe(29)
  })

  it('shows the release date as the calendar day the server sent, not the day before', async () => {
    await show(film('2026-10-30'))
    expect(host.querySelector('.watch__date')?.textContent).toBe('Released on Oct 30, 2026')
  })

  it('shows a date it cannot read as it came', async () => {
    await show(film('unknown'))
    expect(host.querySelector('.watch__date')?.textContent).toBe('Released on unknown')
  })
})
