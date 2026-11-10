// @vitest-environment jsdom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { prefs } from '../api/prefs'
import { SUBTITLES_OFF, type Item, type MediaStream } from '../api/types'

// The Player against a fake <video> (jsdom has no media) and a fake API: what it asks
// the server for, what it reports, and what it tells the subtitle renderer.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  mediaToken: vi.fn(async () => ({ token: 'tok' })),
  streamUrl: (id: string) => `/v1/stream/${id}`,
  remuxUrl: (id: string, audio: number | null, start: number) => `/v1/sources/${id}/remux?start=${start}&audio=${audio}`,
  transcodeUrl: (id: string) => `/v1/sources/${id}/transcode`,
  remuxStart: vi.fn(async (_id: string, start: number) => start),
  progress: vi.fn(async (..._args: unknown[]) => undefined),
  trickplay: vi.fn(async () => null),
  trickplaySheetUrl: () => '',
}))
vi.mock('../api/client', () => ({ api, ApiError: class extends Error {} }))

const subs = vi.hoisted(() => ({
  setTrack: vi.fn(async (_source: string, _stream: unknown) => undefined),
  setOffset: vi.fn((_s: number) => undefined),
  destroy: vi.fn(),
}))
vi.mock('./subtitles', () => ({ createSubtitleRenderer: () => subs }))

import { Player } from './Player'

// ---- a <video> the test drives ----
interface Fake {
  t: number
  paused: boolean
  ended: boolean
  seeking: boolean
  src: string
}
const fake = new WeakMap<HTMLMediaElement, Fake>()
const st = (v: HTMLMediaElement): Fake => {
  let f = fake.get(v)
  if (!f) fake.set(v, (f = { t: 0, paused: true, ended: false, seeking: false, src: '' }))
  return f
}
const play = vi.fn()
const proto = HTMLMediaElement.prototype
Object.defineProperties(proto, {
  currentTime: { configurable: true, get() { return st(this).t }, set(x: number) { st(this).t = x } },
  paused: { configurable: true, get() { return st(this).paused } },
  ended: { configurable: true, get() { return st(this).ended } },
  seeking: { configurable: true, get() { return st(this).seeking } },
  readyState: { configurable: true, get() { return 4 } },
  buffered: { configurable: true, get() { return { length: 0, start: () => 0, end: () => 0 } } },
  src: { configurable: true, get() { return st(this).src }, set(x: string) { st(this).src = x } },
})
proto.play = function (this: HTMLMediaElement) {
  play(st(this).src)
  st(this).paused = false
  this.dispatchEvent(new Event('play'))
  return Promise.resolve()
}
proto.pause = function (this: HTMLMediaElement) {
  st(this).paused = true
}
proto.load = function (this: HTMLMediaElement) {
  st(this).paused = true
}

const DURATION = 1400
function item(id: string, audioCodec: string, userData: Item['userData'] = null): Item {
  const streams: MediaStream[] = [
    { index: 0, type: 'video', codec: 'h264', height: 1080 },
    { index: 1, type: 'audio', codec: audioCodec, language: 'jpn' },
    { index: 2, type: 'subtitle', codec: 'ass', language: 'eng', isTextFormat: true },
    { index: 3, type: 'subtitle', codec: 'hdmv_pgs_subtitle', language: 'eng', isTextFormat: false },
  ]
  return { id, type: 'episode', title: id, userData, mediaSources: [{ id: 'src-' + id, durationMs: DURATION * 1000, streams }] } as Item
}

let root: Root
let host: HTMLDivElement
let setShown: (it: Item) => void = () => undefined
let onEnded: () => void = () => undefined

function Harness({ first }: { first: Item }) {
  const [it, setIt] = useState(first)
  setShown = setIt
  return <Player item={it} onEnded={() => onEnded()} />
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}
const video = () => host.querySelector('video') as HTMLVideoElement
const fire = (type: string) => act(() => { video().dispatchEvent(new Event(type)) })
/** Plays the element's clock from where it is to `to`, a timeupdate every 0.25 s. */
const playTo = (to: number) => {
  const v = video()
  while (st(v).t < to - 1e-9) {
    st(v).t = Math.min(to, st(v).t + 0.25)
    fire('timeupdate')
  }
}
/** A server stream that runs out: Chrome jumps to the header's duration, pauses, ends. */
const runOut = () => {
  st(video()).t = DURATION
  fire('timeupdate')
  st(video()).paused = true
  fire('pause')
}
const reports = () => api.progress.mock.calls.map((c) => ({ item: c[0] as string, ms: c[1] as number, audio: c[2] as number | undefined, sub: c[3] as number | undefined }))
const lastReport = () => reports().at(-1)
const pagehide = () => act(() => { window.dispatchEvent(new Event('pagehide')) })

async function mount(first: Item) {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  await act(async () => root.render(<Harness first={first} />))
  await flush()
  fire('loadstart')
}

beforeEach(() => {
  localStorage.clear()
  prefs.set({ subMode: 'full', audioLang: 'jpn', subLang: 'eng', quality: 'original', autoplay: true, autoSkip: false })
  vi.clearAllMocks()
  api.remuxStart.mockImplementation(async (_id: string, start: number) => start)
  onEnded = () => undefined
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

describe('Player', () => {
  it('reports the last real position after a server stream jumped to the end, not the jump', async () => {
    await mount(item('a', 'ac3')) // AC3: remuxed
    playTo(20)
    runOut()
    pagehide()
    expect(lastReport()?.ms).toBe(20000)
  })

  it('a stream lost for good is saved where it got to, also on leaving the page', async () => {
    await mount(item('a', 'ac3'))
    for (let i = 0; i < 4; i++) {
      playTo(st(video()).t + 5)
      runOut()
      fire('ended')
      await flush()
      if (i === 3) break // the fourth cut in a row: given up, no new stream
      st(video()).t = 0
      fire('loadstart')
    }
    expect(host.textContent).toContain('keeps dropping')
    const at = lastReport()!.ms
    expect(at).toBe(20000)
    // Chrome left the element at the header's duration; leaving now must not save that.
    st(video()).t = DURATION
    api.progress.mockClear()
    pagehide()
    for (const r of reports()) expect(r.ms).toBe(20000)
  })

  it('a seek starts the count of cuts in a row again', async () => {
    await mount(item('a', 'ac3'))
    const cut = async () => {
      playTo(st(video()).t + 5)
      runOut()
      fire('ended')
      await flush()
      st(video()).t = 0
      fire('loadstart')
    }
    await cut()
    await cut()
    act(() => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })) }) // +10 s, out of the buffer: a new stream
    await flush()
    fire('loadstart')
    await cut()
    await cut()
    expect(host.textContent).not.toContain('keeps dropping')
  })

  it('a quality change starts the count of cuts in a row again', async () => {
    await mount(item('a', 'ac3'))
    const cut = async () => {
      playTo(st(video()).t + 5)
      runOut()
      fire('ended')
      await flush()
      st(video()).t = 0
      fire('loadstart')
    }
    await cut()
    await cut()
    const click = (el: Element | null | undefined) => act(() => { (el as HTMLElement).click() })
    click(host.querySelector('[aria-label="Settings"]'))
    click([...host.querySelectorAll('.panel__row')].find((b) => b.textContent?.startsWith('720p')))
    await flush()
    expect(video().src).toContain('/transcode')
    fire('loadstart')
    await cut()
    await cut()
    expect(host.textContent).not.toContain('keeps dropping')
  })

  it('plays the next episode when the last one ended paused (the Player stays mounted)', async () => {
    await mount(item('a', 'aac'))
    expect(play).toHaveBeenCalledTimes(1)
    st(video()).t = DURATION - 2
    fire('seeked')
    playTo(DURATION)
    st(video()).paused = true
    st(video()).ended = true
    await act(async () => setShown(item('b', 'aac')))
    await flush()
    expect(play).toHaveBeenCalledTimes(2)
    expect(play.mock.calls[1][0]).toBe('/v1/stream/src-b')
  })

  it('reports the end once when autoplay moves on', async () => {
    onEnded = () => setShown(item('b', 'aac'))
    await mount(item('a', 'aac'))
    st(video()).t = DURATION - 2
    fire('seeked')
    playTo(DURATION)
    st(video()).paused = true
    st(video()).ended = true
    fire('ended')
    await flush()
    expect(reports().filter((r) => r.item === 'a' && r.ms === DURATION * 1000)).toHaveLength(1)
  })

  it('fetches the subtitles once on open and once for the next episode', async () => {
    await mount(item('a', 'aac'))
    const tracks = () => subs.setTrack.mock.calls.filter((c) => c[1] != null)
    expect(tracks()).toHaveLength(1)
    await act(async () => setShown(item('b', 'aac')))
    await flush()
    expect(tracks()).toHaveLength(2)
    expect(tracks()[1][0]).toBe('src-b')
  })

  it('leaves a saved track it cannot show alone (PGS) and follows the profile', async () => {
    prefs.set({ subMode: 'none' })
    await mount(item('a', 'aac', { positionMs: 0, played: false, isFavorite: false, audioIndex: 1, subtitleIndex: 3 }))
    expect(subs.setTrack.mock.calls.filter((c) => c[1] != null)).toHaveLength(0) // the profile says none
    playTo(12)
    st(video()).paused = true
    fire('pause')
    expect(lastReport()).toMatchObject({ ms: 12000, audio: 1, sub: undefined })
    // Picked here (C turns them on): now it is this player's choice to save.
    act(() => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'c', bubbles: true })) })
    await flush()
    pagehide()
    expect(lastReport()).toMatchObject({ ms: 12000, sub: 2 })
    act(() => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'c', bubbles: true })) })
    await flush()
    pagehide()
    expect(lastReport()).toMatchObject({ sub: SUBTITLES_OFF })
  })

  it('does not save tracks nobody chose (nothing saved, nothing picked)', async () => {
    await mount(item('a', 'aac'))
    playTo(12)
    st(video()).paused = true
    fire('pause')
    expect(lastReport()).toMatchObject({ ms: 12000, audio: undefined, sub: undefined })
  })

  it('a remux clock starts at the keyframe the server reports, not the start point', async () => {
    api.remuxStart.mockImplementation(async () => 600)
    await mount(item('a', 'ac3', { positionMs: 605_000, played: false, isFavorite: false }))
    expect(api.remuxStart).toHaveBeenCalledWith('src-a', 605)
    expect(subs.setOffset.mock.calls.at(-1)?.[0]).toBe(600)
    playTo(5.5) // the element's clock: 5.5 s past the keyframe at 600
    pagehide()
    expect(lastReport()?.ms).toBe(605500)
  })
})
