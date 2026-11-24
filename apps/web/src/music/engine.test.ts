// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Engine, EXACT_MAX_BYTES, EXACT_MAX_SECONDS, exactSeconds, Heard, type EngineEvent, type EngineTrack } from './engine'
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
  currentTime = 100
  sampleRate = FakeContext.rate
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
    return Promise.resolve()
  }
  /** A file's length is told by its size here: a byte a second. */
  decodeAudioData(bytes: ArrayBuffer) {
    return Promise.resolve({ duration: bytes.byteLength })
  }
}

const track = (id: string, over: Partial<EngineTrack> = {}): EngineTrack => ({ id, url: `/s/${id}`, duration: 200, gainDb: 0, exact: true, ...over })
let events: EngineEvent['type'][]
let fetched: string[]
let engine: Engine
const settle = async () => {
  for (let i = 0; i < 6; i++) await Promise.resolve()
}
const el = (id: string) => FakeAudio.made.find((a) => a.src.endsWith('/s/' + id))!
/** The buffer node last put on the clock that was not stopped since. */
const live = () => FakeContext.last!.nodes.filter((n) => !n.stopped)

beforeEach(() => {
  FakeAudio.made = []
  FakeAudio.broken = false
  FakeContext.last = null
  FakeContext.rate = 48000
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
    expect(c2.started!.when).toBeCloseTo(ctx.currentTime + 150.2, 3) // what is left of b
    // A seek moves the end, and the join with it.
    engine.seek(190)
    const c3 = live().at(-1)!
    expect(c3.started!.when).toBeCloseTo(ctx.currentTime + 10, 3)
    expect(c2.stopped).toBe(true)
  })

  it('moves a song from its element to memory at a seek, never in the middle of the sound', async () => {
    engine.load(track('a'), true)
    await settle() // decoded meanwhile, for later
    expect(engine.voice()).toBe('element')
    expect(live().length).toBe(0)
    engine.seek(40)
    expect(engine.voice()).toBe('buffer')
    expect(live()[0].started!.offset).toBe(40)
    expect(el('a').paused).toBe(true)
    expect(events.filter((e) => e === 'paused')).toEqual([]) // the element's pause was the engine's own
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

  it('bounds a decoded song by the memory it takes at the output\'s rate, not by its length alone', async () => {
    // 8 bytes a moment for stereo: at 48 kHz the budget is 11 minutes, at 96 kHz half that,
    // and a slow output never lifts the limit on length.
    expect(exactSeconds(48000)).toBeCloseTo(EXACT_MAX_BYTES / (48000 * 8), 6)
    expect(Math.round(exactSeconds(48000))).toBe(699)
    expect(Math.round(exactSeconds(96000))).toBe(350)
    expect(Math.round(exactSeconds(192000))).toBe(175)
    expect(exactSeconds(8000)).toBe(EXACT_MAX_SECONDS)
    // A six-minute song: decoded for a 48 kHz output, played from an element at 96 kHz.
    const six = track('six', { duration: 360 })
    engine.load(track('a'), true)
    expect(engine.canDecode(six)).toBe(true)
    FakeContext.rate = 96000
    const hi = new Engine()
    hi.load(track('a'), true)
    expect(hi.outputRate()).toBe(96000)
    expect(hi.canDecode(six)).toBe(false)
    expect(hi.canDecode(track('five', { duration: 300 }))).toBe(true)
    fetched.length = 0
    hi.setNext(six)
    await settle()
    expect(fetched).not.toContain('/s/six')
    expect(el('six')).toBeTruthy()
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
