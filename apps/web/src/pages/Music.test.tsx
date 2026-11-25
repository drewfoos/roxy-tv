// @vitest-environment jsdom
import { act, useEffect, useRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MusicTrack } from '../api/music'
import { session } from '../api/session'
import { clearAsyncCache } from '../hooks/useAsync'
import { FakeAudio } from '../music/fakeAudio'
import { musicPrefs } from '../music/prefs'
import { currentTrack, player, resetPlayerForTests } from '../music/player'
import { toast } from '../music/toast'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

vi.mock('../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../components/Footer', () => ({ Footer: () => null }))

import { Music, MusicPlaylistPage, MusicSongPage, visitPick } from './Music'
import { onOpenNowPlaying } from '../music/links'

const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
const song = (id: string, over: Partial<MusicTrack> = {}): MusicTrack => ({
  id,
  title: 'Song ' + id,
  artist: 'Artist ' + id,
  artistId: 'artist-' + id,
  artists: [{ id: 'artist-' + id, name: 'Artist ' + id }],
  durationMs: 200000,
  container: 'mp3',
  codec: 'mp3',
  bitrate: 320000,
  lossless: false,
  size: 1,
  favorite: false,
  playCount: 0,
  addedAt: '',
  ...over,
})
const album = (id: string) => ({ id, title: 'Album ' + id, artistId: 'ar', artist: 'Someone', trackCount: 2, durationMs: 1, addedAt: '', favorite: false })

/** The stand-in server: what it holds, what it was asked, and what it refuses. */
let server: {
  playlists: { id: string; name: string; entries: { entryId: number; track: MusicTrack }[] }[]
  albums: ReturnType<typeof album>[]
  /** The songs the profile likes (null: the two the server holds). */
  liked: MusicTrack[] | null
  /** A server with no music at all. */
  noMusic: boolean
  artists: { id: string; name: string; trackCount: number; albumCount: number; favorite: boolean }[]
  calls:{ method: string; url: string; body?: unknown }[]
  refuse: (method: string, url: string) => boolean
  /** Answers held back until let go (to look at the page while a change is on its way). */
  hold: ((go: () => void) => void) | null
}
const listed = () => server.playlists.map((p) => ({ id: p.id, name: p.name, trackCount: p.entries.length, durationMs: 0, art: [], createdAt: '', updatedAt: '' }))

function answer(method: string, url: string, body: unknown): { status: number; body: unknown } {
  const path = url.split('?')[0]
  const one = /^\/v1\/music\/playlists\/([^/]+)(\/[a-z]+)?$/.exec(path)
  if (path === '/v1/music' && server.noMusic) return { status: 200, body: { counts: { tracks: 0, albums: 0, artists: 0 }, scanning: false } }
  if (path === '/v1/music')
    return {
      status: 200,
      body: {
        counts: { tracks: 5, albums: 1, artists: 3 },
        scanning: false,
        recentlyAdded: [{ album: album('new') }, { track: song('r1') }, { track: song('r2') }],
        recentlyPlayed: [song('p1'), song('p2')],
        mostPlayed: [song('m1', { playCount: 7 })],
      },
    }
  if (path === '/v1/music/playlists' && method === 'GET') return { status: 200, body: { playlists: listed() } }
  if (path === '/v1/music/playlists' && method === 'POST') {
    const made = { id: 'made', name: (body as { name: string }).name, entries: [] }
    server.playlists.push(made)
    return { status: 201, body: listed().at(-1) }
  }
  if (one) {
    const p = server.playlists.find((x) => x.id === one[1])
    if (!p) return { status: 404, body: { error: 'not found' } }
    if (!one[2] && method === 'GET') return { status: 200, body: { playlist: listed().find((x) => x.id === p.id), tracks: p.entries.map((e) => ({ ...e.track, entryId: e.entryId })) } }
    if (!one[2] && method === 'PATCH') p.name = (body as { name: string }).name
    if (!one[2] && method === 'DELETE') server.playlists = server.playlists.filter((x) => x !== p)
    if (one[2] === '/order') p.entries = (body as { entryIds: number[] }).entryIds.map((id) => p.entries.find((e) => e.entryId === id)!)
    if (one[2] === '/remove') p.entries = p.entries.filter((e) => !(body as { entryIds: number[] }).entryIds.includes(e.entryId))
    return { status: 204, body: null }
  }
  if (path === '/v1/music/tracks' && url.includes('favorite=') && server.liked) return { status: 200, body: { tracks: server.liked, total: server.liked.length } }
  if (path === '/v1/music/search') return { status: 200, body: { artists: [], albums: [], tracks: [] } }
  if (path === '/v1/music/tracks/gone') return { status: 404, body: { error: 'track not found' } }
  if (path.startsWith('/v1/music/tracks/') && path.split('/').length === 5) return { status: 200, body: song(path.split('/')[4]) }
  if (path === '/v1/music/tracks') return { status: 200, body: { tracks: [song('a'), song('b')], total: 2 } }
  if (path === '/v1/music/albums') return { status: 200, body: { albums: server.albums } }
  if (path === '/v1/music/artists') return { status: 200, body: { artists: server.artists } }
  return { status: 200, body: {} }
}

let host: HTMLDivElement
let root: Root
let where = ''
const $ = (sel: string) => document.body.querySelector<HTMLElement>(sel)
const $$ = (sel: string) => Array.from(document.body.querySelectorAll<HTMLElement>(sel))
const button = (label: string) => $$('button').find((b) => b.getAttribute('aria-label') === label || b.textContent!.trim() === label) ?? null
const item = (text: string) => $$('.menu [role="menuitem"]').find((b) => b.textContent!.trim().startsWith(text)) ?? null
const click = (el: Element | null) => act(async () => { el!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true })) })
const key = (el: Element | null, k: string) => act(async () => { el!.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })) })
const settle = async () => { for (let i = 0; i < 6; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) }) }
const rows = () => $$('.trow:not(.trow--head) .trow__title').map((t) => t.textContent)
const type = async (text: string) => {
  const input = $('.mdialog input') as HTMLInputElement
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () => { $('.mdialog')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
  await settle()
}

/** Follows a link once (a push, as a press on a link is). */
function Go({ to }: { to: string }) {
  const nav = useNavigate()
  const went = useRef(false)
  useEffect(() => {
    if (went.current) return
    went.current = true
    nav(to)
  }, [nav, to])
  return null
}

function Where() {
  const loc = useLocation()
  where = loc.pathname + loc.search
  return null
}
const open = async (at: string) => {
  await act(async () =>
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <Where />
        <Routes>
          <Route path="/music" element={<Music />} />
          <Route path="/music/playlist/:id" element={<MusicPlaylistPage />} />
          <Route path="/music/song/:id" element={<MusicSongPage />} />
          <Route path="*" element={<p>elsewhere</p>} />
        </Routes>
      </MemoryRouter>,
    ),
  )
  await settle()
}

beforeEach(() => {
  server = {
    playlists: [{ id: 'pl1', name: 'Morning', entries: [{ entryId: 11, track: song('a') }, { entryId: 12, track: song('b') }, { entryId: 13, track: song('a') }, { entryId: 14, track: song('c') }] }],
    albums: [],
    liked: null,
    noMusic: false,
    artists: [],
    calls: [],
    refuse: () => false,
    hold: null,
  }
  FakeAudio.made = []
  FakeAudio.broken = false
  vi.stubGlobal('Audio', FakeAudio)
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} })
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably')
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: { method?: string; body?: string }) => {
      const method = init?.method ?? 'GET'
      const body = init?.body ? JSON.parse(init.body) : undefined
      server.calls.push({ method, url, body })
      // A change that is held has not reached the server: it is made when it is let go.
      const act = () => {
        const a = server.refuse(method, url) ? { status: 500, body: { error: 'no' } } : answer(method, url, body)
        return { ok: a.status < 400, status: a.status, headers: { get: () => 'application/json' }, json: async () => a.body, text: async () => (a.body == null ? '' : JSON.stringify(a.body)) }
      }
      const hold = method !== 'GET' ? server.hold : null
      return hold ? new Promise((resolve) => hold(() => resolve(act()))) : Promise.resolve(act())
    }),
  )
  localStorage.clear()
  musicPrefs.reset()
  session.set(S)
  clearAsyncCache()
  resetPlayerForTests()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  Element.prototype.scrollIntoView = vi.fn()
  Element.prototype.scrollTo = vi.fn() as unknown as typeof Element.prototype.scrollTo
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  document.body.className = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('the music home', () => {
  it('lists what was added as whole tiles in a row: an album as an album, a lone song as a song', async () => {
    await open('/music')
    const row = $$('.row').find((r) => r.querySelector('.row__title')!.textContent === 'Recently added')!
    // The row is the site's own slider, laid out in squares: nothing is clipped to half a tile.
    expect(row.querySelector('.row__track')!.className).toContain('row__track--squares')
    const names = Array.from(row.querySelectorAll('.mtile__name')).map((n) => n.textContent)
    expect(names).toEqual(['Album new', 'Song r1', 'Song r2'])
    expect(row.querySelector('a.mtile__name')!.getAttribute('href')).toBe('/music/album/new')
    // A press on a lone song plays the lone songs, from that one.
    await click(button('Play Song r2'))
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['r1', 'r2'])
    expect(currentTrack()!.id).toBe('r2')
  })

  it('shows the profile\'s playlists in a row of their own, and every tab', async () => {
    await open('/music')
    const titles = $$('.row__title').map((t) => t.textContent)
    expect(titles).toEqual(['Jump back in', 'Recently added', 'Your playlists'])
    const mine = $$('.row').find((r) => r.querySelector('.row__title')!.textContent === 'Your playlists')!
    expect(mine.querySelector('a.mtile__name')!.getAttribute('href')).toBe('/music/playlist/pl1')
    expect(mine.querySelector('.mtile__sub')!.textContent).toBe('4 songs')
    expect($$('.mtabs button').map((b) => b.textContent)).toEqual(['Home', 'Songs', 'Artists', 'Albums', 'Playlists', 'Liked'])
  })

  it('offers some of the albums to explore, the same ones through a refresh, and the artists with the most songs', async () => {
    server.albums = Array.from({ length: 40 }, (_, i) => album('x' + i))
    server.artists = [3, 9, 1, 30, 7].map((n, i) => ({ id: 'ar' + i, name: 'Artist ' + i, trackCount: n, albumCount: 0, favorite: false }))
    await open('/music')
    const row = (title: string) => $$('.row').find((r) => r.querySelector('.row__title')!.textContent === title)!
    const names = (title: string) => Array.from(row(title).querySelectorAll('.mtile__name')).map((n) => n.textContent)
    const first = names('Explore your albums')
    expect(first.length).toBe(24)
    expect(new Set(first).size).toBe(24)
    expect(first).not.toEqual(server.albums.slice(0, 24).map((a) => a.title)) // a pick, not the top of the list
    expect(names('Top artists')).toEqual(['Artist 3', 'Artist 1', 'Artist 4', 'Artist 0', 'Artist 2'])
    expect(row('Top artists').querySelector('.mtile__sub')!.textContent).toBe('30 songs')
    // The list is read again from the server: the tiles stay where they are.
    const asked = () => server.calls.filter((c) => c.url.startsWith('/v1/music/albums')).length
    const before = asked()
    await act(async () => clearAsyncCache())
    await settle()
    expect(asked()).toBeGreaterThan(before)
    expect(names('Explore your albums')).toEqual(first)
    // The pick itself: some of the list, each once, the same for the same visit and another for another.
    const all = server.albums
    expect(visitPick(all, 10, 'one')).toEqual(visitPick([...all].reverse(), 10, 'one'))
    expect(visitPick(all, 10, 'one').map((a) => a.id)).not.toEqual(visitPick(all, 10, 'two').map((a) => a.id))
    expect(visitPick(all, 100, 'one').length).toBe(40)
  })

  it('leaves the playlists row out when there are none', async () => {
    server.playlists = []
    await open('/music')
    expect($$('.row__title').map((t) => t.textContent)).toEqual(['Jump back in', 'Recently added'])
  })
})

describe('the playlists tab', () => {
  it('makes a playlist under the name typed and opens it', async () => {
    await open('/music?tab=playlists')
    expect($$('.mtile__name').map((n) => n.textContent)).toEqual(['Morning'])
    await click(button('New playlist'))
    await type('Night drive')
    expect(server.calls.find((c) => c.method === 'POST' && c.url === '/v1/music/playlists')!.body).toEqual({ name: 'Night drive', trackIds: [] })
    expect(where).toBe('/music/playlist/made')
    expect($('.mhero__title')!.textContent).toBe('Night drive')
    expect($('.mempty')!.textContent).toContain('Nothing in this playlist yet')
  })

  it('says how to make one when there are none, with the one button that makes it', async () => {
    server.playlists = []
    await open('/music?tab=playlists')
    expect($('.mempty')!.textContent).toContain('No playlists yet')
    expect($$('button').filter((b) => b.textContent!.trim() === 'New playlist').length).toBe(1)
    await click($('.mempty button'))
    await type('First')
    expect(where).toBe('/music/playlist/made')
  })

  it('gives an empty page something to look at and a way on: liked songs lead to the songs', async () => {
    server.liked = []
    await open('/music?tab=liked')
    const empty = $('.mempty')!
    expect(empty.querySelector('.mempty__title')!.textContent).toBe('No liked songs yet')
    expect(empty.querySelector('.mempty__icon svg')).not.toBeNull()
    expect(empty.querySelectorAll('.mempty__ring').length).toBe(3)
    await click(empty.querySelector('button'))
    expect(where).toBe('/music?tab=songs')
    expect(rows()).toEqual(['Song a', 'Song b'])
  })

  it('says so, with nothing to press, when a search finds nothing', async () => {
    await open('/music?q=zzz')
    expect($('.mempty__title')!.textContent).toBe('Nothing found for “zzz”')
    expect($('.mempty button, .mempty a')).toBeNull()
    expect($('.mtabs')).toBeNull() // a search stands in for the tabs
  })

  it('offers an admin the way to add a folder when the server has no music, and only an admin', async () => {
    server.noMusic = true
    await open('/music')
    expect($('.mempty__title')!.textContent).toBe('No music yet')
    expect($('.mempty a')).toBeNull()
    await act(async () => root.unmount())
    root = createRoot(host)
    session.set({ ...S, isAdmin: true })
    clearAsyncCache()
    await open('/music')
    expect($('.mempty a')!.getAttribute('href')).toBe('/account/library')
  })

  it('shows a playlist made from a menu elsewhere without leaving the tab', async () => {
    await open('/music?tab=playlists')
    server.playlists.push({ id: 'other', name: 'From a menu', entries: [] })
    const { playlistsChanged } = await import('../music/changes')
    await act(async () => playlistsChanged())
    await settle()
    expect($$('.mtile__name').map((n) => n.textContent)).toEqual(['Morning', 'From a menu'])
  })
})

describe('a playlist', () => {
  it('lists its songs in its order, a song twice where it was added twice, and plays from a row', async () => {
    await open('/music/playlist/pl1')
    expect($('.mhero__title')!.textContent).toBe('Morning')
    expect($('.mhero__facts')!.textContent).toBe('4 songs, 13:20')
    expect(rows()).toEqual(['Song a', 'Song b', 'Song a', 'Song c'])
    await click($$('.trow:not(.trow--head)')[3])
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'b', 'a', 'c'])
    expect(player.get().queue.index).toBe(3)
  })

  it('moves a row at once and tells the server the new order by entry', async () => {
    let go: (() => void) | null = null
    server.hold = (g) => (go = g)
    await open('/music/playlist/pl1')
    await key($$('[data-reorder-handle]')[3], 'ArrowUp')
    // Shown before the server has answered.
    expect(rows()).toEqual(['Song a', 'Song b', 'Song c', 'Song a'])
    expect(server.calls.find((c) => c.method === 'PUT')).toEqual({ method: 'PUT', url: '/v1/music/playlists/pl1/order', body: { entryIds: [11, 12, 14, 13] } })
    await act(async () => go!())
    await settle()
    expect(rows()).toEqual(['Song a', 'Song b', 'Song c', 'Song a'])
    expect(server.playlists[0].entries.map((e) => e.entryId)).toEqual([11, 12, 14, 13])
  })

  it('sends two quick changes one after the other, and shows the second through the first\'s answer', async () => {
    const held: (() => void)[] = []
    server.hold = (g) => held.push(g)
    await open('/music/playlist/pl1')
    await key($$('[data-reorder-handle]')[0], 'ArrowDown') // b a a c
    await key($$('[data-reorder-handle]')[3], 'ArrowUp') //   b a c a
    expect(rows()).toEqual(['Song b', 'Song a', 'Song c', 'Song a'])
    // The second waits for the first: sent together they could arrive the other way round.
    expect(server.calls.filter((c) => c.method === 'PUT').length).toBe(1)
    await act(async () => held[0]())
    await settle()
    expect(rows()).toEqual(['Song b', 'Song a', 'Song c', 'Song a']) // not back to the first change alone
    expect(server.calls.filter((c) => c.method === 'PUT').map((c) => (c.body as { entryIds: number[] }).entryIds)).toEqual([[12, 11, 13, 14], [12, 11, 14, 13]])
    await act(async () => held[1]())
    await settle()
    expect(rows()).toEqual(['Song b', 'Song a', 'Song c', 'Song a'])
  })

  it('takes one entry out from its row\'s menu, not the song\'s other entry', async () => {
    await open('/music/playlist/pl1')
    await click($$('.trow__menu button')[2]) // the second "Song a"
    await click(item('Remove from this playlist'))
    await settle()
    expect(server.calls.find((c) => c.url.endsWith('/remove'))!.body).toEqual({ entryIds: [13] })
    expect(rows()).toEqual(['Song a', 'Song b', 'Song c'])
    expect($('.mhero__facts')!.textContent).toBe('3 songs, 10:00')
  })

  it('keeps the order just made while its change is on the way, whatever else makes the page read the list again', async () => {
    let go: (() => void) | null = null
    server.hold = (g) => (go = g)
    await open('/music/playlist/pl1')
    await key($$('[data-reorder-handle]')[0], 'ArrowDown')
    expect(rows()).toEqual(['Song b', 'Song a', 'Song a', 'Song c'])
    // Something else changes the playlists (a song added from a menu): the page reads
    // its list again, and the server, which has not had the new order yet, sends the old.
    const gets = () => server.calls.filter((c) => c.method === 'GET' && c.url === '/v1/music/playlists/pl1').length
    const before = gets()
    const { playlistsChanged } = await import('../music/changes')
    await act(async () => playlistsChanged())
    await settle()
    expect(gets()).toBeGreaterThan(before)
    expect(rows()).toEqual(['Song b', 'Song a', 'Song a', 'Song c']) // not back to the old order under the pointer
    await act(async () => go!())
    await settle()
    expect(rows()).toEqual(['Song b', 'Song a', 'Song a', 'Song c'])
    expect(server.playlists[0].entries.map((e) => e.entryId)).toEqual([12, 11, 13, 14])
  })

  it('puts the list back at once when a change is refused and the server cannot be read either', async () => {
    await open('/music/playlist/pl1')
    server.refuse = () => true
    await key($$('[data-reorder-handle]')[0], 'ArrowDown')
    await settle()
    expect(rows()).toEqual(['Song a', 'Song b', 'Song a', 'Song c'])
    expect(toast.text()).toBe('That change was not saved')
  })

  it('puts the list back and says so when the server refuses a change', async () => {
    server.refuse = (method) => method === 'PUT'
    await open('/music/playlist/pl1')
    await key($$('[data-reorder-handle]')[0], 'ArrowDown')
    await settle()
    expect(rows()).toEqual(['Song a', 'Song b', 'Song a', 'Song c'])
    expect(toast.text()).toBe('That change was not saved')
  })

  it('takes a new name', async () => {
    await open('/music/playlist/pl1')
    await click(button('Rename'))
    expect(($('.mdialog input') as HTMLInputElement).value).toBe('Morning')
    await type('Early')
    expect(server.calls.find((c) => c.method === 'PATCH')).toEqual({ method: 'PATCH', url: '/v1/music/playlists/pl1', body: { name: 'Early' } })
    expect($('.mhero__title')!.textContent).toBe('Early')
    expect($('.mdialog')).toBeNull()
  })

  it('is deleted only by a second press, and goes back to the playlists', async () => {
    await open('/music/playlist/pl1')
    await click(button('Delete'))
    expect(server.calls.some((c) => c.method === 'DELETE')).toBe(false)
    await click(button('Delete it for good'))
    await settle()
    expect(server.calls.some((c) => c.method === 'DELETE' && c.url === '/v1/music/playlists/pl1')).toBe(true)
    expect(where).toBe('/music?tab=playlists')
    expect(toast.text()).toBe('Deleted Morning')
  })

  it('offers the whole of it for the queue from the menu beside Play', async () => {
    await act(async () => player.playTracks([song('x')], 0, { shuffle: false }))
    await open('/music/playlist/pl1')
    await click(button('More for Morning'))
    await click(item('Add to queue'))
    await settle()
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['x', 'a', 'b', 'a', 'c'])
    expect(toast.text()).toBe('4 songs added to the queue')
  })

  it('says so when it is not there (another profile\'s, or deleted)', async () => {
    await open('/music/playlist/nope')
    expect(host.textContent).toContain('This playlist could not be opened')
  })
})

describe("a song's own address", () => {
  let opened = 0
  let stop = () => {}
  beforeEach(() => {
    opened = 0
    stop = onOpenNowPlaying(() => { opened++ })
  })
  afterEach(() => stop())

  it('shows the song in "now playing", ready to be played and not playing', async () => {
    await open('/music/song/x')
    await settle()
    expect(currentTrack()!.id).toBe('x')
    expect(player.get().playing).toBe(false) // a page does not start sound by itself
    expect(opened).toBe(1)
    expect($('.music')).not.toBeNull() // over the music home
  })

  it('goes to the song where the queue holds it, and leaves the rest of the queue as it was', async () => {
    await act(async () => { player.playTracks([song('a'), song('x'), song('c')], 0, { shuffle: false }); player.pause() })
    await open('/music/song/x')
    await settle()
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'x', 'c'])
    expect(currentTrack()!.id).toBe('x')
    expect(player.get().playing).toBe(false)
    expect(opened).toBe(1)
  })

  it('puts a song the queue does not hold after the one that was current', async () => {
    await act(async () => { player.playTracks([song('a'), song('b')], 0, { shuffle: false }); player.pause() })
    await open('/music/song/x')
    await settle()
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'x', 'b'])
    expect(currentTrack()!.id).toBe('x')
  })

  it('does not take the player over from a song that is playing: the song is put next', async () => {
    await act(async () => player.playTracks([song('a'), song('b')], 0, { shuffle: false }))
    await act(async () =>
      root.render(
        <MemoryRouter initialEntries={['/music', '/music/song/x']} initialIndex={0}>
          <Where />
          <Routes>
            <Route path="/music" element={<Music />} />
            <Route path="/music/song/:id" element={<MusicSongPage />} />
          </Routes>
          <Go to="/music/song/x" />
        </MemoryRouter>,
      ),
    )
    await settle()
    expect(currentTrack()!.id).toBe('a')
    expect(player.get().playing).toBe(true)
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'x', 'b'])
    expect(toast.text()).toBe('"Song x" plays next')
    expect(where).toBe('/music')
    expect(opened).toBe(0)
  })

  it('opens "now playing" for the song that is playing already', async () => {
    await act(async () => player.playTracks([song('a'), song('x')], 1, { shuffle: false }))
    await open('/music/song/x')
    await settle()
    expect(player.get().playing).toBe(true)
    expect(opened).toBe(1)
    expect(where).toBe('/music/song/x')
  })

  it('says so when the song is not in the library, and shows the music home', async () => {
    await open('/music/song/gone')
    await settle()
    expect(toast.text()).toBe('That song is not in the library')
    expect(where).toBe('/music')
    expect(opened).toBe(0)
  })
})
