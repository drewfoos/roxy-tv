// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MusicTrack } from '../api/music'
import { session } from '../api/session'
import { clearAsyncCache } from '../hooks/useAsync'
import { FakeAudio } from './fakeAudio'
import { MusicBar } from './MusicBar'
import { MusicBarHost } from './MusicBarHost'
import { TrackList } from './Parts'
import { currentTrack, player, resetPlayerForTests } from './player'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

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
const flac = song('hi', { title: 'Hi-Res Song', codec: 'flac', container: 'flac', lossless: true, bitDepth: 24, sampleRate: 96000, albumId: 'al', album: 'The Album', lyrics: 'lrc' })
const list = [song('a'), song('b'), flac]

let host: HTMLDivElement
let root: Root
let calls: { method: string; url: string }[]
let failFavorite = false
const $ = (sel: string) => host.querySelector<HTMLElement>(sel)
const $$ = (sel: string) => Array.from(host.querySelectorAll<HTMLElement>(sel))
const button = (label: string) => $$('button').find((b) => b.getAttribute('aria-label') === label) ?? null
const click = (el: Element | null) => act(async () => { el!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true })) })
const settle = () => act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve() })

beforeEach(() => {
  FakeAudio.made = []
  FakeAudio.broken = false
  calls = []
  failFavorite = false
  vi.stubGlobal('Audio', FakeAudio)
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: { method?: string }) => {
      const method = init?.method ?? 'GET'
      calls.push({ method, url })
      if (url.includes('/favorites/') && failFavorite) return Promise.resolve({ ok: false, status: 500, headers: { get: () => 'application/json' }, json: async () => ({ error: 'no' }), text: async () => '{"error":"no"}' })
      const body = url.endsWith('/lyrics')
        ? { source: 'lrc', synced: true, lines: [{ ms: 0, text: 'First line' }, { ms: 30000, text: 'Second line' }, { ms: 60000, text: 'Third line' }] }
        : url === '/v1/music'
          ? { counts: { tracks: 3, albums: 0, artists: 3 }, scanning: false }
          : {}
      return Promise.resolve({ ok: true, status: method === 'GET' ? 200 : 204, headers: { get: () => 'application/json' }, json: async () => body, text: async () => (method === 'GET' ? JSON.stringify(body) : '') })
    }),
  )
  localStorage.clear()
  session.set(S)
  clearAsyncCache()
  resetPlayerForTests()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  // jsdom lays nothing out: these are not there.
  Element.prototype.scrollIntoView = vi.fn()
  Element.prototype.scrollTo = vi.fn() as unknown as typeof Element.prototype.scrollTo
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  document.body.className = ''
  vi.unstubAllGlobals()
})

const renderBar = () => act(async () => root.render(<MemoryRouter><MusicBar /></MemoryRouter>))

describe('the player bar', () => {
  it('shows the song, how it is encoded, and follows play and pause', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    expect($('.mbar__title')!.textContent).toBe('Song a')
    expect($('.mbar__artist a')!.getAttribute('href')).toBe('/music/artist/artist-a')
    expect($('.mbar__quality')!.textContent).toBe('MP3 · 320 kbps')
    expect(button('Pause')).not.toBeNull()
    await click(button('Pause'))
    expect(button('Play')).not.toBeNull()
    expect(FakeAudio.made[0].paused).toBe(true)
    await click(button('Play'))
    expect(FakeAudio.made[0].paused).toBe(false)
  })

  it('marks a hi-res file, links its album, and offers lyrics only where there are some', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    expect(button('Lyrics')).toBeNull()
    expect($('.mbar__album')).toBeNull()
    await act(async () => player.jump(2))
    expect($('.mbar__quality')!.textContent).toBe('FLAC · 24-bit / 96 kHz')
    expect($('.mbar__quality')!.className).toContain('mbar__quality--hires')
    expect($('.mbar__album a')!.getAttribute('href')).toBe('/music/album/al')
    expect(button('Lyrics')).not.toBeNull()
  })

  it('goes to the next and the previous song, shuffles and repeats', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    await click(button('Next'))
    expect($('.mbar__title')!.textContent).toBe('Song b')
    await click(button('Previous'))
    expect($('.mbar__title')!.textContent).toBe('Song a')
    expect(button('Shuffle')!.getAttribute('aria-pressed')).toBe('false')
    await click(button('Shuffle'))
    expect(button('Shuffle')!.getAttribute('aria-pressed')).toBe('true')
    expect(currentTrack()!.id).toBe('a') // the song playing stays
    // The repeat button says what a press will do.
    await click(button('Repeat'))
    expect(player.get().repeat).toBe('all')
    await click(button('Repeat one'))
    expect(player.get().repeat).toBe('one')
    await click(button('Repeat off'))
    expect(player.get().repeat).toBe('off')
  })

  it('shows the time gone and the length, and mutes', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    await act(async () => FakeAudio.made[0].playTo(65, 200))
    const times = $$('.mbar__time').map((t) => t.textContent)
    expect(times).toEqual(['1:05', '3:20'])
    expect($('.mbar__seek .mslider')!.getAttribute('aria-valuetext')).toBe('1:05 of 3:20')
    await click(button('Mute'))
    expect(FakeAudio.made[0].muted).toBe(true)
    expect(button('Unmute')).not.toBeNull()
  })

  it('moves the song with the arrow keys on the seek line', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    await act(async () => FakeAudio.made[0].playTo(20, 200))
    const line = $('.mbar__seek .mslider')!
    await act(async () => { line.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })) })
    expect(FakeAudio.made[0].currentTime).toBe(25)
    await act(async () => { line.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })) })
    expect(FakeAudio.made[0].currentTime).toBe(20)
  })

  it('opens the queue: the songs, a press to jump to one, a press to take one out', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    expect($('.mqueue')).toBeNull()
    await click(button('Queue'))
    expect($$('.qrow').map((r) => r.querySelector('.qrow__title')!.textContent)).toEqual(['Song a', 'Song b', 'Hi-Res Song'])
    expect($('.qrow--current .qrow__title')!.textContent).toBe('Song a')
    await click($$('.qrow')[2])
    expect($('.mbar__title')!.textContent).toBe('Hi-Res Song')
    await click(button('Remove Song b from the queue'))
    expect($$('.qrow').length).toBe(2)
    expect($('.mbar__title')!.textContent).toBe('Hi-Res Song') // the song playing is untouched
    await act(async () => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })) })
    expect($('.mqueue')).toBeNull()
  })

  it('opens "now playing" with the lyrics, the line being sung lit, and a press on a line goes there', async () => {
    await act(async () => player.playTracks(list, 2, { shuffle: false }))
    await renderBar()
    await click(button('Lyrics'))
    await settle()
    expect($('.mnow')).not.toBeNull()
    expect($$('.mlyrics__line').map((l) => l.textContent)).toEqual(['First line', 'Second line', 'Third line'])
    expect($('.mlyrics__line.on')!.textContent).toBe('First line')
    await act(async () => FakeAudio.made[0].playTo(31, 200))
    expect($('.mlyrics__line.on')!.textContent).toBe('Second line')
    await click($$('.mlyrics__line')[2])
    expect(FakeAudio.made[0].currentTime).toBe(60)
    // The other tab is the queue.
    await click($$('.mnow__tabs button').find((b) => b.textContent === 'Queue')!)
    expect($$('.mnow .qrow').length).toBe(3)
    await click(button('Close'))
    expect($('.mnow')).toBeNull()
  })

  it('says when a song would not play', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    await act(async () => FakeAudio.made[0].fail())
    expect($('.mbar__error')!.textContent).toContain('"Song a" could not be played.')
    expect($('.mbar__title')!.textContent).toBe('Song b') // and went on
    await click(button('Dismiss'))
    expect($('.mbar__error')).toBeNull()
  })
})

describe('a list of songs', () => {
  const renderList = () => act(async () => root.render(<MemoryRouter><TrackList tracks={list} /></MemoryRouter>))

  it('plays the list from the song that is pressed, and marks it', async () => {
    await renderList()
    expect($$('.trow').length).toBe(3)
    await click($$('.trow')[1])
    expect(currentTrack()!.id).toBe('b')
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'b', 'hi'])
    expect($$('.trow')[1].className).toContain('trow--current')
    // A press on the song that is playing pauses it; it does not start it again.
    await click($$('.trow')[1])
    expect(player.get().playing).toBe(false)
    expect(currentTrack()!.id).toBe('b')
  })

  it('plays in the order shown even when shuffle is on', async () => {
    await act(async () => player.setShuffle(true))
    await renderList()
    // Shuffled, the song pressed would be moved to the front of the queue.
    await click($$('.trow')[2])
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a', 'b', 'hi'])
    expect(player.get().queue.index).toBe(2)
  })

  it('follows a link in a row without playing the row', async () => {
    await renderList()
    await click($$('.trow')[0].querySelector('.trow__artist a'))
    expect(currentTrack()).toBeNull()
  })

  it('likes a song at once, tells the server, and takes it back when the server refuses', async () => {
    await renderList()
    const heart = () => $$('.trow')[0].querySelector<HTMLElement>('.mheart')!
    expect(heart().getAttribute('aria-pressed')).toBe('false')
    await click(heart())
    expect(heart().getAttribute('aria-pressed')).toBe('true')
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/music/favorites/track/a')).toBe(true)
    expect(currentTrack()).toBeNull() // the heart is not the row
    await click(heart())
    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/music/favorites/track/a')).toBe(true)
    expect(heart().getAttribute('aria-pressed')).toBe('false')
    failFavorite = true
    await click(heart())
    await settle()
    expect(heart().getAttribute('aria-pressed')).toBe('false')
  })

  it('puts a song next, or at the end, from its menu', async () => {
    await act(async () => player.playTracks([song('x'), song('y')], 0, { shuffle: false }))
    await renderList()
    await click(button('More for Song b'))
    await click($$('.menu button').find((b) => b.textContent === 'Play next')!)
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['x', 'b', 'y'])
    expect($('.menu')).toBeNull()
    await click(button('More for Hi-Res Song'))
    await click($$('.menu button').find((b) => b.textContent === 'Add to queue')!)
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['x', 'b', 'y', 'hi'])
  })
})

describe('the bar across pages', () => {
  let go: (to: string) => void = () => undefined
  function Nav() {
    go = useNavigate()
    return null
  }
  const renderApp = (at = '/') =>
    act(async () =>
      root.render(
        <MemoryRouter initialEntries={[at]}>
          <Nav />
          <Routes>
            <Route path="*" element={<p>page</p>} />
          </Routes>
          <MusicBarHost />
        </MemoryRouter>,
      ),
    )

  it('is not there until something is played', async () => {
    await renderApp()
    await settle()
    expect($('.mbar')).toBeNull()
    expect(document.body.classList.contains('has-mbar')).toBe(false)
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await settle()
    await settle()
    expect($('.mbar__title')!.textContent).toBe('Song a')
    expect(document.body.classList.contains('has-mbar')).toBe(true)
  })

  it('stays on another page, and steps aside for a video, pausing the music', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderApp('/music')
    await settle()
    await settle()
    await act(async () => go('/series/x'))
    expect($('.mbar__title')!.textContent).toBe('Song a')
    expect(player.get().playing).toBe(true)
    await act(async () => go('/watch/episode-1'))
    expect($('.mbar')).toBeNull()
    expect(player.get().playing).toBe(false)
    expect(FakeAudio.made[0].paused).toBe(true)
    expect(document.body.classList.contains('has-mbar')).toBe(false)
    // Back from the video: the bar returns, still paused.
    await act(async () => go('/'))
    await settle()
    expect($('.mbar__title')!.textContent).toBe('Song a')
    expect(player.get().playing).toBe(false)
  })
})
