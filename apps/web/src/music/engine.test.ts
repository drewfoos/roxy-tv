// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { decodedBytes, Engine, EXACT_MAX_SECONDS, exactBudget, exactSeconds, HANDOVER_SECONDS, Heard, type EngineEvent, type EngineTrack } from './engine'
import { FakeAudio } from './fakeAudio'

// A stand-in for Web Audio: its clock is moved by the test, and every start and stop of a
// buffer is written down with the time it was put on the clock for.
class FakeNode {
  buffer: { duration: number } | null = null
  onended: (() => void) | null = null
  started: { when: number; offset: number } | null = null
  stopped = false
  constructor(private ctx: FakeContext) {}
  connect() {}
  disconnect() {}
  start(when: number, offset: number) {
    this.started = { when, offset }
    this.ctx.nodes.push(this)
  }
  stop() {
    this.stopped = true
  }
  /** The buffer played to its end. */
  end() {
    this.onended?.()
  }
}
class FakeContext {
  static last: FakeContext | null = null
  static rate = 48000
  static latency: number | undefined = undefined
  /** Set, decodes wait here until let go (to look at the engine while one runs). */
  static holdDecode: ((go: () => void) => void) | null = null
  /** How many channels a decoded file turns out to have. */
  static channels = 2
  baseLatency = FakeContext.latency
  currentTime = 100
  sampleRate = FakeContext.rate
  state = 'running'
  onstatechange: (() => void) | null = null
  resumed = 0
  destination = {}
  nodes: FakeNode[] = []
  constructor() {
    FakeContext.last = this
  }
  createGain() {
    return { gain: { value: 1, setTargetAtTime() {} }, connect() {}, disconnect() {} }
  }
  createAnalyser() {
    return { fftSize: 0, smoothingTimeConstant: 0, connect() {} }
  }
  createMediaElementSource() {
    return { connect() {} }
  }
  createBufferSource() {
    return new FakeNode(this)
  }
  resume() {
    this.resumed++
    this.state = 'running'
    return Promise.resolve()
  }
  /** A file's length is told by its size here: a byte a second, in stereo at this context's rate. */
  decodeAudioData(bytes: ArrayBuffer) {
    const buffer = { duration: bytes.byteLength, length: bytes.byteLength * this.sampleRate, numberOfChannels: FakeContext.channels }
    const hold = FakeContext.holdDecode
    return hold ? new Promise((resolve) => hold(() => resolve(buffer))) : Promise.resolve(buffer)
  }
}

const MB = 1024 * 1024
const track = (id: string, over: Partial<EngineTrack> = {}): EngineTrack => ({ id, url: `/s/${id}`, duration: 200, gainDb: 0, exact: true, ...over })
let events: EngineEvent['type'][]
let fetched: string[]
let engine: Engine
/** How far ahead a start is asked for here (the stand-in output says nothing of its own latency). */
const LEAD = 0.06
const settle = async () => {
  for (let i = 0; i < 40; i++) await Promise.resolve()
}
const el = (id: string) => FakeAudio.made.find((a) => a.src.endsWith('/s/' + id))!
/** The buffer node last put on the clock that was not stopped since. */
const live = () => FakeContext.last!.nodes.filter((n) => !n.stopped)

beforeEach(() => {
  FakeAudio.made = []
  FakeAudio.broken = false
  FakeContext.last = null
  FakeContext.rate = 48000
  FakeContext.latency = undefined
  FakeContext.holdDecode = null
  FakeContext.channels = 2
  events = []
  fetched = []
  vi.stubGlobal('Audio', FakeAudio)
  vi.stubGlobal('AudioContext', FakeContext)
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      fetched.push(url)
      return Promise.resolve({ ok: true, arrayBuffer: async () => new ArrayBuffer(200) })
    }),
  )
  engine = new Engine()
  engine.on((e) => events.push(e.type))
})
afterEach(() => vi.unstubAllGlobals())

describe('what was heard', () => {
  it('adds up what was played, each part once', () => {
    const h = new Heard()
    h.open(0)
    h.close(60)
    h.open(30)
    h.close(90) // half of it again
    h.open(150)
    expect(h.total(170)).toBe(110) // 0-90, and 150 to where it is now
    h.close(170)
    h.open(80)
    h.close(160) // joins the two
    expect(h.total()).toBe(170)
    h.close(999) // not open: nothing
    expect(h.total()).toBe(170)
  })
})

describe('songs that follow each other', () => {
  it('joins a decoded song to the one before it on the audio clock, to the sample', async () => {
    const ctx = () => FakeContext.last!
    engine.load(track('a'), true)
    expect(engine.voice()).toBe('element') // the first song starts at once, from an element
    engine.setNext(track('b'))
    await settle()
    expect(fetched).toContain('/s/b')
    // The element nears its end: the decoded song is put on the clock for the moment it ends.
    const a = el('a')
    a.duration = 200
    a.currentTime = 199.8
    ctx().currentTime = 300
    a.emit('timeupdate')
    const b = live()[0]
    expect(b.started!.when).toBeCloseTo(300.2, 5)
    expect(b.started!.offset).toBe(0)
    a.finish()
    expect(events).toContain('advanced')
    expect(engine.voice()).toBe('buffer')
    expect(engine.stats).toEqual({ exactJoins: 0, clockJoins: 1, plainJoins: 0 })

    // From a decoded song to the next: at the exact end of the one playing.
    engine.setNext(track('c'))
    await settle()
    const c = live().find((n) => n !== b)!
    expect(c.started!.when).toBe(b.started!.when + 200) // the end of b, whatever the time is now
    ctx().currentTime = 500.2
    b.end()
    expect(engine.stats.exactJoins).toBe(1)
    expect(engine.position()).toBeCloseTo(0, 5)
    expect(c.stopped).toBe(false)
  })

  it('keeps the song being decoded when it is named as next again meanwhile', async () => {
    // The player says what follows at every tick of the clock, each time with a new
    // object: the decode started for the first telling is the one that is used.
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    engine.setNext(track('b', { gainDb: -6 }))
    await settle()
    engine.setNext(track('b', { gainDb: -6 }))
    expect(fetched.filter((u) => u === '/s/b').length).toBe(1)
    const a = el('a')
    a.duration = 200
    a.currentTime = 199.9
    a.emit('timeupdate')
    const b = live()[0]
    expect(b?.started).toBeTruthy() // on the clock: it was not thrown away
    a.finish()
    expect(engine.voice()).toBe('buffer')
    expect(engine.stats.clockJoins).toBe(1)
    expect(engine.stats.plainJoins).toBe(0)
    expect(FakeAudio.made.some((x) => x.src.endsWith('/s/b'))).toBe(false) // never loaded a second way
  })

  it('never plays one song\'s sound as another: a decode that arrives after the queue changed is dropped', async () => {
    // b is being decoded when c becomes what follows; b's file arrives first, c's later.
    // Here a file's length is its size: b is 111 seconds, c 222.
    let sendC: () => void = () => undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        fetched.push(url)
        const answer = { ok: true, arrayBuffer: async () => new ArrayBuffer(url.endsWith('/b') ? 111 : 222) }
        return url.endsWith('/c') ? new Promise((resolve) => (sendC = () => resolve(answer))) : Promise.resolve(answer)
      }),
    )
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    engine.setNext(track('c'))
    await settle() // b has arrived: it is not what follows any more
    sendC()
    await settle()
    const a = el('a')
    a.duration = 200
    a.currentTime = 199.9
    a.emit('timeupdate')
    expect(live().length).toBe(1)
    expect(live()[0].buffer!.duration).toBe(222) // c's own sound
    a.finish()
    expect(engine.duration()).toBe(222)
  })

  it('drops a decode that is no longer wanted when it arrives', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    engine.setNext(track('c', { exact: false })) // the queue changed before b was decoded
    await settle()
    el('a').finish()
    expect(engine.voice()).toBe('element')
    expect(el('c').paused).toBe(false)
    expect(live().length).toBe(0)
  })

  it('takes the next song off the clock for a pause, and puts it back for the new end', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle()
    const a = el('a')
    a.duration = 200
    a.currentTime = 199.8
    a.emit('timeupdate')
    a.finish() // b plays from a buffer now
    const ctx = FakeContext.last!
    engine.setNext(track('c'))
    await settle()
    const first = live().at(-1)!
    ctx.currentTime += 50
    engine.pause()
    expect(first.stopped).toBe(true) // c would otherwise start while b is paused
    expect(engine.position()).toBeCloseTo(49.8, 3) // b began 0.2 s after it was put on the clock
    ctx.currentTime += 1000 // a long pause
    engine.play()
    const [b2, c2] = live().slice(-2)
    expect(b2.started!.offset).toBeCloseTo(49.8, 3)
    expect(c2.started!.when).toBeCloseTo(ctx.currentTime + LEAD + 150.2, 3) // what is left of b, from when it really starts again
    // A seek moves the end, and the join with it.
    engine.seek(190)
    const c3 = live().at(-1)!
    expect(c3.started!.when).toBeCloseTo(ctx.currentTime + LEAD + 10, 3)
    expect(c2.stopped).toBe(true)
  })

  it('moves a song from its element to memory at a seek, never in the middle of the sound', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle() // both decoded meanwhile: b to follow, a to take over from its element
    expect(fetched).toEqual(['/s/b', '/s/a']) // the one that follows first: the join needs it
    expect(engine.voice()).toBe('element')
    expect(live().length).toBe(0)
    engine.seek(40)
    expect(engine.voice()).toBe('buffer')
    const [a, b] = live()
    expect(a.started!.offset).toBe(40)
    expect(b.started!.when).toBe(a.started!.when + 160) // and b is on the clock for a's end, to the sample
    expect(el('a').paused).toBe(true)
    expect(events.filter((e) => e === 'paused')).toEqual([]) // the element's pause was the engine's own
  })

  it('does not decode the song playing when nothing is to follow it exactly', async () => {
    engine.load(track('a'), true)
    await settle()
    expect(fetched).toEqual([]) // a second download and its memory, for a seek that may never come
    expect(engine.memory()).toEqual({ songs: 0, bytes: 0 })
    engine.setNext(track('b', { exact: false }))
    await settle()
    expect(fetched).toEqual([])
    engine.seek(40)
    expect(engine.voice()).toBe('element')
  })

  it('plays songs that need no exact join through elements alone', async () => {
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b', { exact: false }))
    await settle()
    expect(fetched).toEqual([]) // nothing decoded into memory
    expect(el('b').paused).toBe(true)
    el('a').finish()
    expect(el('b').paused).toBe(false)
    expect(engine.stats.plainJoins).toBe(1)
    expect(engine.voice()).toBe('element')
  })

  it('does not decode what is too long to hold in memory', async () => {
    const long = track('long', { duration: EXACT_MAX_SECONDS + 1 })
    expect(engine.canDecode(long)).toBe(false)
    engine.load(track('a'), true)
    engine.setNext(long)
    await settle()
    expect(fetched).not.toContain('/s/long')
    expect(el('long')).toBeTruthy() // made ready in an element instead
  })

  it('bounds a decoded song by the memory it takes at the output\'s rate, and by what the device has', async () => {
    const MB = 1024 * 1024
    // What a device is given: a browser that says how much memory it has is taken at its word.
    expect(exactBudget(8, false)).toBe(256 * MB)
    expect(exactBudget(16, true)).toBe(256 * MB)
    expect(exactBudget(4, false)).toBe(128 * MB)
    expect(exactBudget(2, false)).toBe(0) // too little: songs join through elements
    expect(exactBudget(undefined, false)).toBe(192 * MB) // not told, a computer
    expect(exactBudget(undefined, true)).toBe(64 * MB) // not told, a phone or tablet
    // 8 bytes a moment for stereo: at 48 kHz 256 MB is 11 minutes, at 96 kHz half that,
    // and a slow output never lifts the limit on length.
    expect(Math.round(exactSeconds(48000, 256 * MB))).toBe(699)
    expect(Math.round(exactSeconds(96000, 256 * MB))).toBe(350)
    expect(Math.round(exactSeconds(192000, 256 * MB))).toBe(175)
    expect(Math.round(exactSeconds(48000, 64 * MB))).toBe(175)
    expect(exactSeconds(8000, 256 * MB)).toBe(EXACT_MAX_SECONDS)
    expect(exactSeconds(48000, 0)).toBe(0)
    // Here (not told, a computer: 192 MB) a six-minute song is decoded for a 48 kHz
    // output, and played from an element for a 96 kHz one, where it would take 276 MB.
    const six = track('six', { duration: 360 })
    engine.load(track('a'), true)
    expect(engine.canDecode(six)).toBe(true)
    FakeContext.rate = 96000
    const hi = new Engine()
    hi.load(track('a'), true)
    expect(hi.outputRate()).toBe(96000)
    expect(hi.canDecode(six)).toBe(false)
    expect(hi.canDecode(track('four', { duration: 240 }))).toBe(true)
    fetched.length = 0
    hi.setNext(six)
    await settle()
    expect(fetched).not.toContain('/s/six')
    expect(el('six')).toBeTruthy()
    // A device with little memory decodes nothing at all.
    vi.stubGlobal('navigator', { deviceMemory: 2 })
    FakeContext.rate = 48000
    const small = new Engine()
    small.load(track('a'), true)
    expect(small.canDecode(track('short', { duration: 30 }))).toBe(false)
  })

  it('starts a decoded song a little ahead and on a sample, so that it ends when it is reckoned to', async () => {
    // What the browser hands the system at a time here: 30 ms. A start asked for "now"
    // would happen up to that much later than the clock read on the page says.
    FakeContext.latency = 0.03
    FakeContext.rate = 44100
    const e = new Engine()
    e.load(track('a'), true)
    e.setNext(track('b'))
    await settle()
    const ctx = FakeContext.last!
    ctx.currentTime = 100.0000123 // a time that is on no sample (half of one past)
    e.seek(40.00001) // nor is this place in the song
    const [a, b] = live()
    const onSample = (t: number) => Math.abs(t * 44100 - Math.round(t * 44100)) < 1e-6
    expect(a.started!.when).toBeGreaterThanOrEqual(ctx.currentTime + 0.05) // the 30 ms, and 20 more
    expect(a.started!.when).toBeLessThan(ctx.currentTime + 0.0501)
    expect(onSample(a.started!.when)).toBe(true) // between two samples the whole song would be played between samples
    expect(onSample(a.started!.offset)).toBe(true)
    // The next song is on the clock for the sample this one ends at: what is left of it, from when it really starts.
    expect(onSample(b.started!.when)).toBe(true)
    expect(Math.round((b.started!.when - a.started!.when) * 44100)).toBe(Math.round((200 - a.started!.offset) * 44100))
    // Until the start comes the song stands where it will start from, not before it.
    expect(e.position()).toBe(a.started!.offset)
    ctx.currentTime = a.started!.when + 5
    expect(e.position()).toBeCloseTo(a.started!.offset + 5, 9)
    // An output that says nothing of its latency is given 40 ms, and no output more than 300.
    FakeContext.latency = undefined
    expect(LEAD).toBe(0.06)
    FakeContext.latency = 5
    const slow = new Engine()
    slow.load(track('a'), true)
    slow.setNext(track('b'))
    await settle()
    const c2 = FakeContext.last!
    slow.seek(10)
    expect(live().find((n) => n.started!.offset === 10)!.started!.when).toBeCloseTo(c2.currentTime + 0.3, 3)
  })

  it('holds two decoded songs at most, and lets one go the moment it is neither playing nor next', async () => {
    const perSong = 200 * 48000 * 2 * 4 // 200 seconds, stereo, 4 bytes a sample
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle()
    expect(engine.memory()).toEqual({ songs: 2, bytes: 2 * perSong })
    // b takes over; c is made ready: a is gone at once, not kept for a press on Previous.
    const a = el('a')
    a.duration = 200
    a.currentTime = 199.9
    a.emit('timeupdate')
    a.finish()
    expect(engine.memory()).toEqual({ songs: 1, bytes: perSong })
    engine.setNext(track('c'))
    await settle()
    expect(engine.memory().songs).toBe(2)
    // What follows changes: the song that was to follow is let go, the new one held.
    engine.setNext(track('d'))
    await settle()
    expect(engine.memory().songs).toBe(2)
    expect(fetched.filter((u) => u === '/s/d').length).toBe(1)
    // Nothing follows: only the song playing is held.
    engine.setNext(null)
    expect(engine.memory()).toEqual({ songs: 1, bytes: perSong })
    // The album is over and another song plays: nothing of it is left behind.
    engine.load(track('x', { exact: false }), true)
    expect(engine.memory()).toEqual({ songs: 0, bytes: 0 })
  })

  it('lets the song before go at once when the one made ready is played by a press', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle()
    expect(engine.memory().songs).toBe(2)
    engine.load(track('b'), true) // Next was pressed: b plays from memory, a is not kept for Previous
    expect(engine.voice()).toBe('buffer')
    expect(engine.memory().songs).toBe(1)
  })

  it('never has two songs being decoded at once, even when the first is no longer wanted', async () => {
    // A decode cannot be stopped once the browser has the file. With b's under way, c
    // becomes what follows: c's file is not asked for until b's decode is over, or both
    // files and both decoded songs would be in memory together.
    const waiting: (() => void)[] = []
    FakeContext.holdDecode = (go) => waiting.push(go)
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    expect(fetched).toEqual(['/s/b'])
    expect(waiting.length).toBe(1) // b is being decoded
    engine.setNext(track('c'))
    await settle()
    expect(fetched).toEqual(['/s/b']) // c waits its turn
    waiting[0]()
    await settle()
    expect(fetched).toEqual(['/s/b', '/s/c'])
    expect(engine.memory().songs).toBe(0) // b was decoded for nothing, and is not kept
    waiting[1]()
    await settle()
    expect(engine.memory().songs).toBe(1)
  })

  it('counts every channel of a recording, not two', async () => {
    // 5.1 takes three times what stereo does: a third of the length fits.
    expect(Math.round(exactSeconds(48000, 256 * MB, 6))).toBe(233)
    expect(Math.round(exactSeconds(48000, 256 * MB, 1))).toBe(1398 > EXACT_MAX_SECONDS ? EXACT_MAX_SECONDS : 1398)
    expect(exactSeconds(48000, 256 * MB)).toBe(exactSeconds(48000, 256 * MB, 2))
    expect(decodedBytes(200, 48000, 6)).toBe(200 * 48000 * 6 * 4)
    expect(decodedBytes(200, 48000)).toBe(200 * 48000 * 2 * 4)
    engine.load(track('a'), true)
    // Here a song may take 192 MB: 200 seconds of stereo is 73 MB, of 5.1 it is 220 MB.
    expect(engine.canDecode(track('stereo', { duration: 200, channels: 2 }))).toBe(true)
    expect(engine.canDecode(track('surround', { duration: 200, channels: 6 }))).toBe(false)
    expect(engine.canDecode(track('short surround', { duration: 100, channels: 6 }))).toBe(true)
    expect(engine.canDecode(track('not told', { duration: 200, channels: undefined }))).toBe(true)
    fetched.length = 0
    engine.setNext(track('surround', { duration: 200, channels: 6 }))
    await settle()
    expect(fetched).not.toContain('/s/surround')
    expect(el('surround')).toBeTruthy()
  })

  it('goes by what the decoder hands back, not by what the library said of the file', async () => {
    // The library says stereo; the file holds six channels. Decoded it takes 220 MB.
    FakeContext.channels = 6
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    expect(fetched).toEqual(['/s/b'])
    expect(engine.memory()).toEqual({ songs: 0, bytes: 0 })
    expect(engine.canDecode(track('b'))).toBe(false)
    // Not downloaded and decoded a second time to find the same thing out.
    engine.setNext(track('b'))
    engine.load(track('a2', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    expect(fetched).toEqual(['/s/b'])
    // It follows through an element, like any song that is not held decoded.
    expect(el('b')).toBeTruthy()
  })

  it('does not keep a song that turns out longer than it was said to be', async () => {
    // Said to be 200 seconds; the file decodes to 600 (220 MB in stereo).
    vi.stubGlobal('fetch', vi.fn((url: string) => (fetched.push(url), Promise.resolve({ ok: true, arrayBuffer: async () => new ArrayBuffer(600) }))))
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    expect(engine.memory().songs).toBe(0)
    expect(engine.canDecode(track('b'))).toBe(false)
  })

  it('does not download a file larger than a decoded song may be', async () => {
    let read = 0
    const stops: AbortSignal[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: { signal?: AbortSignal }) => {
        fetched.push(url)
        if (init?.signal) stops.push(init.signal)
        return Promise.resolve({ ok: true, headers: { get: (k: string) => (k === 'Content-Length' ? String(300 * MB) : null) }, arrayBuffer: async () => (read++, new ArrayBuffer(200)) })
      }),
    )
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    expect(fetched).toEqual(['/s/b'])
    expect(read).toBe(0) // the answer's body was never read
    expect(stops[0]?.aborted).toBe(true) // and the transfer was let go
    expect(engine.memory().songs).toBe(0)
    expect(engine.canDecode(track('b'))).toBe(false)
    expect(engine.pendingFetches()).toBe(0)
  })

  it('keeps hold of a fetch when an earlier one for the same song settles late', async () => {
    const asked: { url: string; signal?: AbortSignal; send: () => void }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: { signal?: AbortSignal }) => new Promise((resolve, reject) => {
        const entry = { url, signal: init?.signal, send: () => resolve({ ok: true, arrayBuffer: async () => new ArrayBuffer(200) }) }
        init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
        asked.push(entry)
      })),
    )
    const release: (() => void)[] = []
    FakeContext.holdDecode = (go) => release.push(go)
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    asked[0].send() // b's file is here and is being decoded
    await settle()
    expect(release.length).toBe(1)
    // b is let go while it is decoded (that cannot be stopped), then wanted again: a
    // second fetch for the same address, waiting its turn behind the first decode.
    engine.setNext(track('c', { exact: false }))
    engine.setNext(track('b'))
    await settle()
    expect(engine.pendingFetches()).toBe(1)
    // The first decode ends now. Its being over must not take the second fetch's entry.
    release[0]()
    await settle()
    expect(asked.map((x) => x.url)).toEqual(['/s/b', '/s/b'])
    expect(engine.pendingFetches()).toBe(1)
    // So that the second can still be stopped when b is let go once more.
    engine.setNext(track('d', { exact: false }))
    expect(asked[1].signal?.aborted).toBe(true)
    await settle()
    expect(engine.pendingFetches()).toBe(0)
    expect(engine.memory().songs).toBe(0)
  })

  it('fetches and decodes one song at a time, and stops fetching one that is let go', async () => {
    const asked: { url: string; signal?: AbortSignal; send: () => void }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: { signal?: AbortSignal }) => new Promise((resolve, reject) => {
        const entry = { url, signal: init?.signal, send: () => resolve({ ok: true, arrayBuffer: async () => new ArrayBuffer(200) }) }
        init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
        asked.push(entry)
      })),
    )
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle()
    // Only b is on its way: a's file (and the memory for it) waits its turn.
    expect(asked.map((x) => x.url)).toEqual(['/s/b'])
    asked[0].send()
    await settle()
    expect(asked.map((x) => x.url)).toEqual(['/s/b', '/s/a'])
    expect(asked[1].signal?.aborted).toBe(false)
    // Another song plays before a's file is here: the fetch is stopped, nothing is kept.
    engine.load(track('x', { exact: false }), true)
    expect(asked[1].signal?.aborted).toBe(true)
    await settle()
    expect(engine.memory().songs).toBe(0)
    // A song let go while it waited its turn is never fetched at all.
    engine.load(track('m'), true)
    engine.setNext(track('n'))
    engine.setNext(track('o'))
    await settle()
    expect(asked.map((x) => x.url)).toEqual(['/s/b', '/s/a', '/s/o'])
  })

  it('puts the next song on the clock a few seconds ahead and keeps the moment up to date until just before', async () => {
    const ctx = () => FakeContext.last!
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    const a = el('a')
    a.duration = 200
    // Further from the end than the hand-over reaches: nothing yet.
    a.currentTime = 200 - HANDOVER_SECONDS - 0.5
    a.emit('timeupdate')
    expect(live().length).toBe(0)
    a.currentTime = 197.5
    ctx().currentTime = 500
    a.emit('timeupdate')
    const first = live()[0]
    expect(first.started!.when).toBeCloseTo(502.5, 6)
    // A second later the element is 10 ms behind where it was reckoned to be: the start moves with it.
    a.currentTime = 198.49
    ctx().currentTime = 501
    a.emit('timeupdate')
    const second = live()[0]
    expect(first.stopped).toBe(true)
    expect(second).not.toBe(first)
    expect(second.started!.when).toBeCloseTo(502.51, 6)
    // A reading that agrees to within what can be heard moves nothing.
    a.currentTime = 199.001
    ctx().currentTime = 501.51
    a.emit('timeupdate')
    expect(live()[0]).toBe(second)
    // Nor does one in the last moment, when a node moved might start late.
    a.currentTime = 199.95
    ctx().currentTime = 502.4
    a.emit('timeupdate')
    expect(live()[0]).toBe(second)
    a.finish()
    expect(engine.stats).toEqual({ exactJoins: 0, clockJoins: 1, plainJoins: 0 })
  })

  it('joins on the clock in a tab whose events come once a second', async () => {
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    const a = el('a')
    a.duration = 200
    const ctx = FakeContext.last!
    // Readings a second apart, none of them in the last third of a second.
    for (const t of [195.6, 196.6, 197.6, 198.6, 199.6]) {
      a.currentTime = t
      ctx.currentTime = 400 + t
      a.emit('timeupdate')
    }
    expect(live().length).toBe(1)
    expect(live()[0].started!.when).toBeCloseTo(600, 6)
    a.finish()
    expect(engine.stats.clockJoins).toBe(1)
    expect(engine.stats.plainJoins).toBe(0)
  })

  it('takes the next song off the clock when the one playing stops to load, and puts it back when it plays again', async () => {
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    const a = el('a')
    const ctx = FakeContext.last!
    a.duration = 200
    a.currentTime = 198
    ctx.currentTime = 300
    a.emit('timeupdate')
    const planned = live()[0]
    expect(planned.started!.when).toBeCloseTo(302, 6)
    // The connection drops with two seconds to go: a will end later than that.
    a.readyState = 2
    a.emit('waiting')
    expect(planned.stopped).toBe(true) // b must not start over a song that has not ended
    expect(live().length).toBe(0)
    expect(events.at(-1)).toBe('waiting')
    ctx.currentTime = 309 // nine seconds of loading
    a.emit('timeupdate')
    expect(live().length).toBe(0) // still loading: nothing is reckoned from a clock that stands still
    a.readyState = 4
    a.emit('playing')
    a.currentTime = 198.3
    ctx.currentTime = 309.3
    a.emit('timeupdate')
    expect(live()[0].started!.when).toBeCloseTo(311, 6)
    a.finish()
    expect(engine.stats.clockJoins).toBe(1)
  })

  it('takes the next song off the clock when the one playing is paused by something else', async () => {
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle()
    const a = el('a')
    a.duration = 200
    a.currentTime = 198.5
    a.emit('timeupdate')
    const planned = live()[0]
    expect(planned).toBeTruthy()
    a.pause() // a headset button, a second and a half before the end
    expect(events.at(-1)).toBe('paused')
    expect(planned.stopped).toBe(true) // or b would start by itself under a paused player
    expect(live().length).toBe(0)
    // Played again: on the clock again for the new end.
    const ctx = FakeContext.last!
    ctx.currentTime = 900
    engine.play()
    a.emit('timeupdate')
    expect(live()[0].started!.when).toBeCloseTo(901.5, 6)
  })

  it('puts a song on the clock at once when it is decoded with the one before it already near its end', async () => {
    let send: () => void = () => undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        fetched.push(url)
        return new Promise((resolve) => (send = () => resolve({ ok: true, arrayBuffer: async () => new ArrayBuffer(200) })))
      }),
    )
    engine.load(track('a', { exact: false }), true)
    engine.setNext(track('b'))
    await settle() // b's file is asked for
    const a = el('a')
    const ctx = FakeContext.last!
    a.duration = 200
    a.currentTime = 199
    ctx.currentTime = 700
    a.emit('timeupdate') // b is not here yet
    expect(live().length).toBe(0)
    send()
    await settle()
    expect(live()[0].started!.when).toBeCloseTo(701, 6) // without waiting for another reading
  })

  it('shows a pause when the sound output is taken away, and leaves nothing on the clock', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b'))
    await settle()
    engine.seek(10) // a plays from memory, b is on the clock for its end
    const ctx = FakeContext.last!
    const [a, b] = live()
    ctx.currentTime += 20
    ctx.state = 'interrupted' // a phone call
    ctx.onstatechange!()
    expect(events.at(-1)).toBe('paused')
    expect(a.stopped && b.stopped).toBe(true)
    expect(engine.position()).toBeCloseTo(30 - LEAD, 3) // twenty seconds after a start that came a moment after it was asked for
    // The output is back: nothing starts by itself. A press on Play goes on from there.
    ctx.state = 'running'
    ctx.onstatechange!()
    expect(live().length).toBe(0)
    engine.play()
    expect(live()[0].started!.offset).toBeCloseTo(30 - LEAD, 3)
    // An output that has not run yet is not one that was taken away.
    FakeContext.last = null
    const fresh = new Engine()
    const seen: string[] = []
    fresh.on((e) => seen.push(e.type))
    fresh.load(track('z', { exact: false }), true)
    const c2 = FakeContext.last! as FakeContext
    c2.currentTime = 0
    c2.state = 'suspended'
    c2.onstatechange!()
    expect(seen).not.toContain('paused')
  })

  it('plays what follows as soon as it can when it was not ready in time', async () => {
    engine.load(track('a'), true)
    engine.setNext(track('b')) // still being fetched and decoded
    el('a').finish()
    expect(events).toContain('advanced')
    expect(engine.voice()).toBe('element')
    expect(el('b').paused).toBe(false)
  })

  it('tells a pause by something else from its own', () => {
    engine.load(track('a', { exact: false }), true)
    engine.load(track('b', { exact: false }), true) // the engine pauses a itself
    expect(events).not.toContain('paused')
    el('b').pause() // a headset button
    expect(events.at(-1)).toBe('paused')
    el('b').play() // and again
    expect(events.at(-1)).toBe('playing')
    expect(events).toContain('resumed')
  })

  it('turns a song down or up by its level', () => {
    engine.setVolume(0.5, false)
    engine.load(track('a', { exact: false, gainDb: -6 }), true)
    // Through the audio graph the element stays at full level (the graph sets the level).
    expect(el('a').volume).toBe(1)
    // Without Web Audio the element's own volume is all there is: down, never up.
    vi.stubGlobal('AudioContext', undefined)
    const plain = new Engine()
    plain.setVolume(0.5, false)
    plain.load(track('p', { exact: false, gainDb: -6 }), true)
    expect(el('p').volume).toBeCloseTo(0.5 * 0.501, 2)
    plain.setGain(6)
    expect(el('p').volume).toBe(0.5)
  })
})
