// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connection, PROBE_DELAYS_MS } from '../api/connection'
import { session } from '../api/session'
import { clearAsyncCache } from '../hooks/useAsync'

// The player itself needs a real browser (see e2e/): here it only counts how often it is
// built, which is what must not happen to a video that is playing.
const mounts = vi.fn()
vi.mock('../player/Player', async () => {
  const { useEffect } = await import('react')
  return {
    Player: ({ item }: { item: { title: string } }) => {
      useEffect(() => { mounts() }, [])
      return <div className="player">{item.title}</div>
    },
  }
})
vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))

import { Watch } from './Watch'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
const film = { id: 'm1', type: 'movie', title: 'Spirited Away', sources: [{ id: 's1', path: '/m/a.mkv', streams: [] }] }
const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body })

let host: HTMLDivElement
let root: Root
let up = false
let itemCalls = 0
const flush = () => act(async () => { await vi.advanceTimersByTimeAsync(0) })

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  session.set(S)
  clearAsyncCache()
  connection.reset()
  mounts.mockClear()
  up = false
  itemCalls = 0
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (!up) return Promise.reject(new TypeError('Failed to fetch'))
    if (url === '/healthz') return Promise.resolve(ok({ status: 'ok' }))
    if (url.startsWith('/v1/items/m1')) { itemCalls++; return Promise.resolve(ok(film)) }
    return Promise.resolve(ok({ items: [] }))
  }))
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  connection.reset()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  session.set(null)
})

const page = () => (
  <MemoryRouter initialEntries={['/watch/m1']}>
    <Routes>
      <Route path="/watch/:id" element={<Watch />} />
    </Routes>
  </MemoryRouter>
)

describe('the watch page and a server that comes back', () => {
  it('opened while the server was away, loads by itself when it answers', async () => {
    act(() => root.render(page()))
    await flush()
    expect(host.querySelector('.offline h2')?.textContent).toBe("Can't reach your Roxy server")
    up = true
    await act(async () => { await vi.advanceTimersByTimeAsync(PROBE_DELAYS_MS[0]) })
    await flush()
    expect(host.querySelector('.player')?.textContent).toBe('Spirited Away')
  })

  it('does not ask for the item again, or rebuild the player, when the server blinks during a video', async () => {
    up = true
    act(() => root.render(page()))
    await flush()
    expect(host.querySelector('.player')).not.toBeNull()
    expect([itemCalls, mounts.mock.calls.length]).toEqual([1, 1])
    // The server goes away and comes back under the playing video.
    connection.failed()
    await act(async () => { await vi.advanceTimersByTimeAsync(PROBE_DELAYS_MS[0]) })
    await flush()
    expect(connection.get()).toBe('ok')
    expect([itemCalls, mounts.mock.calls.length]).toEqual([1, 1])
  })
})
