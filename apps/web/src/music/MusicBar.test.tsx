// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MusicTrack } from '../api/music'
import { session } from '../api/session'
import { clearAsyncCache } from '../hooks/useAsync'
import { FakeAudio } from './fakeAudio'
import { musicPrefs } from './prefs'
import { LEAVE_MS, MusicBar, nowPlayingLine } from './MusicBar'
import { MusicBarHost } from './MusicBarHost'
import { AlbumTile, TrackList } from './Parts'
import { toast } from './toast'
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
let calls: { method: string; url: string; body?: unknown }[]
let playlists: { id: string; name: string; trackCount: number; durationMs: number; art: string[] }[]
let failFavorite = false
// Menus and small windows are drawn into the page's body, beside the host.
const $ = (sel: string) => document.body.querySelector<HTMLElement>(sel)
const $$ = (sel: string) => Array.from(document.body.querySelectorAll<HTMLElement>(sel))
const item = (text: string) => $$('.menu [role="menuitem"]').find((b) => b.textContent!.trim().startsWith(text)) ?? null
const button = (label: string) => $$('button').find((b) => b.getAttribute('aria-label') === label) ?? null
const click = (el: Element | null) => act(async () => { el!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true })) })
const settle = () => act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve() })

beforeEach(() => {
  FakeAudio.made = []
  FakeAudio.broken = false
  calls = []
  playlists = [{ id: 'pl1', name: 'Morning', trackCount: 4, durationMs: 1, art: [] }]
  failFavorite = false
  vi.stubGlobal('Audio', FakeAudio)
  // jsdom has no canvas to draw on.
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
  // jsdom plays nothing: here the browser plays every file as it is.
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably')
  musicPrefs.reset()
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: { method?: string; body?: string }) => {
      const method = init?.method ?? 'GET'
      calls.push({ method, url, body: init?.body ? JSON.parse(init.body) : undefined })
      if (url === '/v1/music/playlists' && method === 'POST') {
        const made = { id: 'new', name: (JSON.parse(init!.body!) as { name: string }).name, trackCount: 0, durationMs: 0, art: [] }
        return Promise.resolve({ ok: true, status: 201, headers: { get: () => 'application/json' }, json: async () => made, text: async () => JSON.stringify(made) })
      }
      if (url.includes('/favorites/') && failFavorite) return Promise.resolve({ ok: false, status: 500, headers: { get: () => 'application/json' }, json: async () => ({ error: 'no' }), text: async () => '{"error":"no"}' })
      const body = url.endsWith('/lyrics')
        ? { source: 'lrc', synced: true, lines: [{ ms: 0, text: 'First line' }, { ms: 30000, text: 'Second line' }, { ms: 60000, text: 'Third line' }] }
        : url === '/v1/music'
          ? { counts: { tracks: 3, albums: 0, artists: 3 }, scanning: false }
          : url === '/v1/music/playlists'
            ? { playlists }
            : url === '/v1/music/albums/al'
              ? { album: { id: 'al', title: 'The Album' }, tracks: [song('m'), song('n')] }
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
  vi.restoreAllMocks()
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
    await click(button('More for Song b'))
    await click(item('Remove from the queue'))
    expect($$('.qrow').length).toBe(2)
    expect($('.mbar__title')!.textContent).toBe('Hi-Res Song') // the song playing is untouched
    await act(async () => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })) })
    expect($('.mqueue')).toBeNull()
  })

  const key = (el: Element, k: string) => act(async () => { el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })) })
  const titles = () => $$('.qrow').map((r) => r.querySelector('.qrow__title')!.textContent)

  it('puts the queue in another order: by the arrow keys on a handle, and from a row\'s menu', async () => {
    await act(async () => player.playTracks([...list, song('d')], 0, { shuffle: false }))
    await renderBar()
    await click(button('Queue'))
    // Down on the third row's handle: it changes places with the fourth.
    await key(button('Move Hi-Res Song')!, 'ArrowDown')
    expect(titles()).toEqual(['Song a', 'Song b', 'Song d', 'Hi-Res Song'])
    await key(button('Move Hi-Res Song')!, 'ArrowUp')
    await key(button('Move Hi-Res Song')!, 'ArrowUp')
    expect(titles()).toEqual(['Song a', 'Hi-Res Song', 'Song b', 'Song d'])
    // The first row cannot go further up, and the song playing is still the one playing.
    await key(button('Move Song a')!, 'ArrowUp')
    expect(titles()).toEqual(['Song a', 'Hi-Res Song', 'Song b', 'Song d'])
    expect(currentTrack()!.id).toBe('a')
    // A press on a handle is not a press on its row.
    await click(button('Move Song d'))
    expect(currentTrack()!.id).toBe('a')
    // "Move to next" is offered for a song further down, not for the one already next.
    await click(button('More for Hi-Res Song'))
    expect(item('Move to next')).toBeNull()
    expect(item('Move to the end')).not.toBeNull()
    await click(button('More for Hi-Res Song'))
    await click(button('More for Song d'))
    expect(item('Move to the end')).toBeNull()
    await click(item('Move to next'))
    expect(titles()).toEqual(['Song a', 'Song d', 'Hi-Res Song', 'Song b'])
  })

  it('moves a queue row by dragging its handle to where the pointer is let go', async () => {
    await act(async () => player.playTracks([...list, song('d')], 0, { shuffle: false }))
    await renderBar()
    await click(button('Queue'))
    // jsdom lays nothing out: each row is given its place, 50 px a row.
    $$('.qrow').forEach((r, i) => { r.getBoundingClientRect = () => ({ top: i * 50, bottom: i * 50 + 50, left: 0, right: 300, width: 300, height: 50, x: 0, y: i * 50, toJSON: () => ({}) }) })
    const pointer = (el: Element, type: string, y: number) => act(async () => { el.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, clientY: y, button: 0 })) })
    const grip = button('Move Song a')!
    await pointer(grip, 'pointerdown', 20)
    expect($$('.qrow')[0].className).toContain('is-dragged')
    await pointer(grip, 'pointermove', 130) // past the middle of the third row
    expect($$('.qrow')[2].className).toContain('drop-after')
    await pointer(grip, 'pointerup', 130)
    expect(titles()).toEqual(['Song b', 'Hi-Res Song', 'Song a', 'Song d'])
    expect(currentTrack()!.id).toBe('a') // it went with its row
    expect($('.is-dragged')).toBeNull()
    // Let go where it was taken up: nothing moves.
    await pointer(button('Move Song d')!, 'pointerdown', 170)
    await pointer(button('Move Song d')!, 'pointerup', 170)
    expect(titles()).toEqual(['Song b', 'Hi-Res Song', 'Song a', 'Song d'])
  })

  it('saves the queue as a playlist under the name typed', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    await click(button('Queue'))
    await click(button('Save the queue as a playlist'))
    const input = $('.mdialog input') as HTMLInputElement
    const save = $$('.mdialog button').find((b) => b.textContent === 'Save')! as HTMLButtonElement
    expect(save.disabled).toBe(true) // no name yet
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '  Evening  ')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => { $('.mdialog')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
    await settle()
    expect(calls.find((c) => c.method === 'POST' && c.url === '/v1/music/playlists')!.body).toEqual({ name: 'Evening', trackIds: ['a', 'b', 'hi'] })
    expect($('.mdialog')).toBeNull()
    expect(toast.text()).toBe('Saved as Evening')
  })

  it('opens "now playing" by a press on the song\'s names where the screen is narrow, and follows their links where it is wide', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    const narrow = (yes: boolean) => vi.stubGlobal('matchMedia', (q: string) => ({ matches: yes && q === '(max-width: 860px)' }))
    narrow(false)
    await click(document.body.querySelector('.mbar__title'))
    expect(document.body.querySelector('.mnow')).toBeNull()
    narrow(true)
    await click(document.body.querySelector('.mbar__title'))
    expect(document.body.querySelector('.mnow')).not.toBeNull()
    // The sheet has the whole transport (the narrow bar has only play and next).
    expect(Array.from(document.body.querySelectorAll('.mnow__controls button')).map((b) => b.getAttribute('aria-label'))).toEqual(['Shuffle', 'Previous', 'Pause', 'Next', 'Repeat'])
    expect(document.body.querySelector('.mnow__controls .mslider')!.getAttribute('aria-label')).toBe('Seek')
  })

  it('opens and closes "now playing" from the cover, whatever tab it is on, its arrow turning to say which', async () => {
    await act(async () => player.playTracks(list, 2, { shuffle: false }))
    await renderBar()
    const cover = () => document.body.querySelector<HTMLElement>('.mbar__cover')!
    const bar = () => document.body.querySelector('.mbar')!.className
    expect(cover().getAttribute('aria-label')).toBe('Now playing')
    expect(bar()).not.toContain('mbar--now')
    await click(cover())
    expect(document.body.querySelector('.mnow')).not.toBeNull()
    expect(cover().getAttribute('aria-label')).toBe('Close now playing')
    expect(bar()).toContain('mbar--now') // the arrow points down: a press closes
    // On another tab than the one the cover opens: the cover still closes.
    await click(document.body.querySelector('.mnow__switch button[aria-label="Queue"]'))
    await click(cover())
    expect(document.body.querySelector('.mnow')).toBeNull()
    expect(bar()).not.toContain('mbar--now')
    // The queue being open is not "now playing" being open: the arrow still says "open".
    await click(button('Queue'))
    expect(bar()).not.toContain('mbar--now')
    expect(cover().getAttribute('aria-label')).toBe('Now playing')
  })

  it('lets a closed panel leave the way it came before it is taken away, and takes it away at once where motion is unwanted', async () => {
    vi.useFakeTimers()
    try {
      let reduced = false
      vi.stubGlobal('matchMedia', (q: string) => ({ matches: reduced && q.includes('prefers-reduced-motion') }))
      await act(async () => player.playTracks(list, 0, { shuffle: false }))
      await renderBar()
      const cover = () => document.body.querySelector<HTMLElement>('.mbar__cover')!
      const now = () => document.body.querySelector('.mnow')
      await click(cover())
      expect(now()!.className).not.toContain('mnow--closing')
      await click(cover())
      expect(now()!.className).toContain('mnow--closing') // still drawn, on its way out
      expect(document.body.querySelector('.mbar')!.className).not.toContain('mbar--now') // the arrow has turned already
      await act(async () => { vi.advanceTimersByTime(LEAVE_MS - 20) })
      expect(now()).not.toBeNull()
      await act(async () => { vi.advanceTimersByTime(40) })
      expect(now()).toBeNull()
      // Opened again while it leaves: it stays, and is not taken away under the pointer.
      await click(cover())
      await click(cover())
      await act(async () => { vi.advanceTimersByTime(100) })
      await click(cover())
      expect(now()!.className).not.toContain('mnow--closing')
      await act(async () => { vi.advanceTimersByTime(LEAVE_MS * 2) })
      expect(now()).not.toBeNull()
      // The queue leaves the same way; with it open, "now playing" opening sends it off.
      await click(cover())
      await act(async () => { vi.advanceTimersByTime(LEAVE_MS + 10) })
      await click(button('Queue'))
      await click(button('Close the queue'))
      expect(document.body.querySelector('.mqueue')!.className).toContain('mqueue--closing')
      await act(async () => { vi.advanceTimersByTime(LEAVE_MS + 10) })
      expect(document.body.querySelector('.mqueue')).toBeNull()
      // Less motion asked for: gone at once.
      reduced = true
      await click(cover())
      await click(cover())
      expect(now()).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens the visualizer from the bar: it fills the panel, its style behind a button in its corner', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)
    const all = (sel: string) => Array.from(document.body.querySelectorAll<HTMLElement>(sel))
    await click(q('.mbar button[aria-label="Visualizer"]'))
    expect(q('.mnow .mviz canvas')).not.toBeNull()
    // What is beside the song is chosen from icons in the panel's corner, each with its name.
    expect(all('.mnow__switch button').map((b) => [b.getAttribute('aria-label'), b.getAttribute('aria-selected')])).toEqual([['Lyrics', 'false'], ['Visualizer', 'true'], ['Queue', 'false']])
    expect(all('.mnow__switch button').every((b) => b.querySelector('svg') && !b.textContent)).toBe(true)
    // No row of styles over the picture: they are in a menu.
    expect(q('.menu')).toBeNull()
    await click(q('.mviz__tools button[aria-label="Visualizer style"]'))
    const styles = () => all('.menu [role="menuitemradio"]')
    expect(styles().map((b) => b.textContent)).toEqual(['Bars', 'Mirror', 'Wave', 'Ring'])
    expect(styles().find((b) => b.getAttribute('aria-checked') === 'true')!.textContent).toBe('Bars')
    await click(styles()[3])
    expect(musicPrefs.get().visualizer).toBe('ring')
    expect(styles().find((b) => b.getAttribute('aria-checked') === 'true')!.textContent).toBe('Ring') // it stays open, to try another
    await act(async () => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect(q('.menu')).toBeNull()
    expect(q('.mnow')).not.toBeNull() // Escape closed the menu, not the player under it
    // It can take the whole of "now playing", the song stepping aside, and come back.
    expect(q('.mnow')!.className).not.toContain('mnow--wide')
    await click(q('.mviz__tools button[aria-label="Fill the window"]'))
    expect(q('.mnow')!.className).toContain('mnow--wide')
    expect(q('.mviz__caption')!.textContent).toBe('Song a · Artist a')
    // Only the visualizer does: on another tab the song is back beside it.
    await click(q('.mnow__switch button[aria-label="Queue"]'))
    expect(q('.mnow')!.className).not.toContain('mnow--wide')
    await click(q('.mnow__switch button[aria-label="Visualizer"]'))
    await click(q('.mviz__tools button[aria-label="Back beside the song"]'))
    expect(q('.mnow')!.className).not.toContain('mnow--wide')
    expect(q('.mviz__caption')).toBeNull()
    // A second press on the bar's button closes it.
    await click(q('.mbar button[aria-label="Visualizer"]'))
    expect(q('.mnow')).toBeNull()
  })

  it('keeps the visualizer still where less motion is asked for, until it is asked for here', async () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('prefers-reduced-motion') }))
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)
    await click(q('.mbar button[aria-label="Visualizer"]'))
    expect(q('.mnow .mviz')).not.toBeNull()
    expect(q('.mnow .mviz canvas')).toBeNull()
    expect(q('.mviz__still')!.textContent).toContain('less motion')
    await click(q('.mviz__still button'))
    expect(q('.mnow .mviz canvas')).not.toBeNull()
    expect(q('.mviz__still')).toBeNull()
  })

  it('says the song to a screen reader when it changes', async () => {
    await act(async () => player.playTracks(list, 0, { shuffle: false }))
    await renderBar()
    const said = () => document.body.querySelector('.mbar [role="status"][aria-live="polite"]')!.textContent
    expect(said()).toBe(nowPlayingLine(list[0]))
    expect(said()).toContain(list[0].title)
    await act(async () => player.next())
    expect(said()).toBe(nowPlayingLine(list[1]))
    expect(nowPlayingLine({ title: 'Song', artist: 'Somebody' })).toBe('Now playing: Song, by Somebody')
    expect(nowPlayingLine({ title: 'Song' })).toBe('Now playing: Song')
  })

  it('changes how songs are sent from the playback settings, and says what this song gets', async () => {
    await act(async () => player.playTracks(list, 2, { shuffle: false }))
    await renderBar()
    expect(FakeAudio.made[0].src).not.toContain('format=')
    await click(button('Playback settings'))
    const facts = () => Object.fromEntries($$('.mset__facts dt').map((dt) => [dt.textContent, dt.nextElementSibling!.textContent]))
    expect(facts()['File']).toBe('FLAC · 24-bit / 96 kHz')
    expect(facts()['Sent as']).toBe('The file itself')
    const quality = (label: string) => $$('.mset__options button').find((b) => b.querySelector('b')!.textContent === label)!
    await click(quality('Low'))
    expect(musicPrefs.get().quality).toBe(128)
    expect(quality('Low').getAttribute('aria-checked')).toBe('true')
    expect(FakeAudio.made.at(-1)!.src).toContain('format=opus')
    expect(FakeAudio.made.at(-1)!.src).toContain('bitrate=128')
    expect(facts()['Sent as']).toBe('Opus, 128 kbit/s (converted)')
    expect($('.mbar__quality')!.textContent).toBe('Opus · 128 kbps')
    expect($('.mbar__quality')!.className).not.toContain('hires') // what is heard is not hi-res any more
    // What is held decoded in memory is said, when anything is (here nothing: no sound output to decode for).
    expect(facts()['In memory']).toBeUndefined()
    const held = vi.spyOn(player, 'memory').mockReturnValue({ songs: 2, bytes: 148 * 1024 * 1024 })
    await act(async () => musicPrefs.set({ visualizer: 'mirror' })) // anything that draws the panel again
    expect(facts()['In memory']).toContain('2 songs, 148 MB')
    held.mockReturnValue({ songs: 1, bytes: 70.4 * 1024 * 1024 })
    await act(async () => musicPrefs.set({ visualizer: 'bars' }))
    expect(facts()['In memory']).toContain('One song, 70 MB')
    held.mockRestore()
    const switches = $$('.mset__switch input') as HTMLInputElement[]
    expect(switches.map((i) => i.checked)).toEqual([true, true])
    await click(switches[0])
    expect(musicPrefs.get().levelling).toBe('off')
    expect(facts()['Levelling']).toBe('Off')
    await click(switches[1])
    expect(musicPrefs.get().gapless).toBe(false)
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
    // The line being sung is named as the current one, not only lit.
    expect($$('.mlyrics__line[aria-current="true"]').map((l) => l.textContent)).toEqual(['Second line'])
    await click($$('.mlyrics__line')[2])
    expect(FakeAudio.made[0].currentTime).toBe(60)
    // The other tab is the queue.
    await click(document.body.querySelector('.mnow__switch button[aria-label="Queue"]'))
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

  it('can be played from the keyboard: each row has a button that is the row\'s own press', async () => {
    await renderList()
    const buttons = () => $$('.trow:not(.trow--head) button.trow__n')
    expect(buttons().map((b) => b.getAttribute('aria-label'))).toEqual(list.map((t) => 'Play ' + t.title))
    expect($$('.trow[aria-current]').length).toBe(0)
    await click(buttons()[1]) // Enter or Space on a button is a press on it
    expect(currentTrack()!.id).toBe('b')
    expect(player.get().playing).toBe(true)
    expect(buttons()[1].getAttribute('aria-label')).toBe('Pause ' + list[1].title)
    expect($$('.trow[aria-current="true"]').length).toBe(1)
    expect($$('.trow')[1].getAttribute('aria-current')).toBe('true')
    await click(buttons()[1])
    expect(player.get().playing).toBe(false)
    expect(buttons()[1].getAttribute('aria-label')).toBe('Play ' + list[1].title)
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
    await click(item('Play next'))
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['x', 'b', 'y'])
    expect($('.menu')).toBeNull()
    expect(toast.text()).toBe('Playing next')
    await click(button('More for Hi-Res Song'))
    await click(item('Add to queue'))
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['x', 'b', 'y', 'hi'])
    expect(toast.text()).toBe('Added to the queue')
    expect(currentTrack()!.id).toBe('x') // neither started anything
  })

  it('opens the same menu with the right mouse button, and closes it with Escape or a press elsewhere', async () => {
    await renderList()
    await act(async () => { $$('.trow')[1].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 40, clientY: 60 })) })
    expect(item('Play next')).not.toBeNull()
    expect(item('Go to artist')).not.toBeNull()
    expect(item('Go to album')).toBeNull() // Song b has no album
    await act(async () => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect($('.menu')).toBeNull()
    await act(async () => { $$('.trow')[2].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true })) })
    expect(item('Go to album')).not.toBeNull()
    await act(async () => { document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })) })
    expect($('.menu')).toBeNull()
    expect(currentTrack()).toBeNull()
  })

  it('closes a menu by a second press on its own ⋮, and moves it by a press on another row\'s', async () => {
    await renderList()
    // A real press is a pointer going down, then a click.
    const press = async (el: Element) => {
      await act(async () => { el.dispatchEvent(new Event('pointerdown', { bubbles: true })) })
      await click(el)
    }
    const open = (title: string) => button('More for ' + title)!.getAttribute('aria-expanded')
    await press(button('More for Song b')!)
    expect(open('Song b')).toBe('true')
    expect(item('Play next')).not.toBeNull()
    await press(button('More for Song b')!)
    expect(document.body.querySelector('.menu')).toBeNull() // closed, not closed and opened again
    expect(open('Song b')).toBe('false')
    await press(button('More for Song b')!)
    await press(button('More for Song a')!)
    expect([open('Song a'), open('Song b')]).toEqual(['true', 'false'])
    expect(document.body.querySelectorAll('.menu').length).toBe(1)
    // A press inside the menu does not close it before the entry is chosen.
    await press(item('Add to queue')!)
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['a'])
  })

  it('adds a song to one of the profile\'s playlists from its menu', async () => {
    await renderList()
    await click(button('More for Song b'))
    await click(item('Add to playlist'))
    await settle()
    expect(item('New playlist')).not.toBeNull()
    expect(item('Morning')!.textContent).toContain('4')
    await click(item('Morning'))
    await settle()
    expect(calls.find((c) => c.method === 'POST' && c.url === '/v1/music/playlists/pl1/tracks')!.body).toEqual({ trackIds: ['b'] })
    expect(toast.text()).toBe('Added 1 song to Morning')
    expect($('.menu')).toBeNull()
  })

  it('makes a new playlist with the song in it, and a press inside the name window does not close it', async () => {
    await renderList()
    await click(button('More for Hi-Res Song'))
    await click(item('Add to playlist'))
    await settle()
    await click(item('New playlist'))
    expect($('.menu')).toBeNull() // the window stands in for the menu
    const input = $('.mdialog input') as HTMLInputElement
    await act(async () => { input.dispatchEvent(new Event('pointerdown', { bubbles: true })) })
    expect($('.mdialog')).not.toBeNull()
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'Late')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => { $('.mdialog')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
    await settle()
    expect(calls.find((c) => c.method === 'POST' && c.url === '/v1/music/playlists')!.body).toEqual({ name: 'Late', trackIds: ['hi'] })
    expect(toast.text()).toBe('Added 1 song to Late')
    expect($('.mdialog')).toBeNull()
  })

  it('drags a row to another place under a line of column names, which is not one of the rows', async () => {
    const moved: [number, number][] = []
    await act(async () => root.render(<MemoryRouter><TrackList tracks={[...list, song('d')]} header onMove={(a, b) => moved.push([a, b])} /></MemoryRouter>))
    // jsdom lays nothing out: the names take the first 36 px, each song 50 px under them.
    const place = (el: HTMLElement, top: number, height: number) => { el.getBoundingClientRect = () => ({ top, bottom: top + height, left: 0, right: 300, width: 300, height, x: 0, y: top, toJSON: () => ({}) }) }
    place(document.body.querySelector<HTMLElement>('.trow--head')!, 0, 36)
    const songRows = () => Array.from(document.body.querySelectorAll<HTMLElement>('.trow:not(.trow--head)'))
    songRows().forEach((r, i) => place(r, 36 + i * 50, 50))
    const pointer = (el: Element, type: string, y: number) => act(async () => { el.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, clientY: y, button: 0 })) })
    const grip = button('Move Song a')!
    await pointer(grip, 'pointerdown', 60)
    await pointer(grip, 'pointermove', 170) // past the middle of the third song (136 to 186)
    expect(songRows()[2].className).toContain('drop-after')
    expect(document.body.querySelector('.trow--head')!.className).not.toContain('drop')
    await pointer(grip, 'pointerup', 170)
    expect(moved).toEqual([[0, 2]])
    // Taken up and let go in its own place: nothing is asked for.
    await pointer(button('Move Song b')!, 'pointerdown', 110)
    await pointer(button('Move Song b')!, 'pointermove', 118)
    await pointer(button('Move Song b')!, 'pointerup', 118)
    expect(moved).toEqual([[0, 2]])
    // The first row has nowhere to go up to, the last nowhere to go down to.
    await act(async () => { button('Move Song a')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true })) })
    await act(async () => { button('Move Song d')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })) })
    expect(moved).toEqual([[0, 2]])
    // The arrow keys keep the keyboard on the handle of the place the row went to.
    const raf = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((f) => { f(0); return 0 })
    await act(async () => { button('Move Song b')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })) })
    expect(moved).toEqual([[0, 2], [1, 2]])
    expect(document.activeElement).toBe(button('Move Hi-Res Song')) // the handle at the third place
    raf.mockRestore()
    expect(currentTrack()).toBeNull()
  })

  it('shows the names of the columns, and a handle on each row only where rows can be moved', async () => {
    const moved: [number, number][] = []
    await act(async () => root.render(<MemoryRouter><TrackList tracks={list} header onMove={(a, b) => moved.push([a, b])} rowMenu={(t) => [{ label: 'Remove ' + t.id, onSelect: () => moved.push([-1, -1]) }]} /></MemoryRouter>))
    expect($('.trow--head')!.textContent).toContain('Title')
    expect($$('.trow:not(.trow--head)').length).toBe(3)
    await act(async () => { button('Move Song a')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })) })
    expect(moved).toEqual([[0, 1]])
    expect(currentTrack()).toBeNull()
    // The row's own entry is in its menu.
    await click(button('More for Song b'))
    await click(item('Remove b'))
    expect(moved).toEqual([[0, 1], [-1, -1]])
    await renderList()
    expect(button('Move Song a')).toBeNull()
    expect($('.trow--head')).toBeNull()
  })
})

describe('a tile', () => {
  it('plays an album from the button on its picture, or puts it in the queue from its menu', async () => {
    const album = { id: 'al', title: 'The Album', artistId: 'ar', artist: 'Someone', trackCount: 2, durationMs: 1, addedAt: '', favorite: false }
    await act(async () => root.render(<MemoryRouter><AlbumTile album={album} /></MemoryRouter>))
    expect($('.mtile__name')!.getAttribute('href')).toBe('/music/album/al')
    await click(button('Play The Album'))
    await settle()
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['m', 'n'])
    expect(player.get().playing).toBe(true)
    await click(button('More for The Album'))
    expect(item('Shuffle')).not.toBeNull()
    await click(item('Add to queue'))
    await settle()
    expect(player.get().queue.tracks.map((t) => t.id)).toEqual(['m', 'n', 'm', 'n'])
    expect(toast.text()).toBe('2 songs added to the queue')
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
