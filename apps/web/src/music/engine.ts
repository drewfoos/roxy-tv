// The sound: one song playing, and the next one made ready so that it follows without a
// gap. Nothing here knows about the queue; the player (player.ts) says what plays and
// what comes next, and hears back what happened.
//
// A song plays through one of two kinds of voice:
//  - an <audio> element: starts at once, streams, costs no memory. The default.
//  - a decoded buffer (Web Audio): the whole song decoded in memory and started at an
//    exact time on the audio clock. Used where a join has to be exact: between songs
//    that follow each other on an album, whose sound runs on from one into the next.
//
// How one song joins the next:
//  - buffer to buffer: the next is scheduled on the audio clock at the sample the current
//    one ends. Exact.
//  - element to buffer: the next is scheduled from the element's own clock a moment
//    before it ends. Within a few milliseconds.
//  - element to element (no Web Audio, or songs that need no exact join): the next
//    element, already loaded, is started when the current one says it ended.
//
// Pauses: the player's own pauses of an element are marked on that element, so a pause
// reported by an element that was not marked is someone else's (a headset button, the
// browser): no timing is guessed at.

export interface EngineTrack {
  id: string
  url: string
  /** The length the library gives, in seconds (the file's own is used once known). */
  duration: number
  /** How much to turn this song up or down, in dB (volume levelling). */
  gainDb: number
  /** Worth decoding into memory for an exact join (it runs on into, or from, its neighbour). */
  exact: boolean
}

export type EngineEvent =
  | { type: 'time' }
  /** Sound is coming out. */
  | { type: 'playing' }
  /** Play was asked for and the sound has stopped to load. */
  | { type: 'waiting' }
  /** Paused, or started again, by something other than the player. */
  | { type: 'paused' }
  | { type: 'resumed' }
  /** The song ended and nothing was set to follow. */
  | { type: 'ended' }
  /** The song set as next is now the one playing. `exact`: joined on the audio clock. */
  | { type: 'advanced'; exact: boolean }
  /** The song could not be played. */
  | { type: 'error' }
  /** The browser refused to start sound nobody asked for with a press. */
  | { type: 'blocked' }

/** Seconds of a song that were played, each part counted once. */
export class Heard {
  private ranges: [number, number][] = []
  private from: number | null = null
  /** Sound starts coming out at `position`. */
  open(position: number) {
    if (this.from == null) this.from = position
  }
  /** Sound stops (a pause, a seek away, the end) at `position`. */
  close(position: number) {
    if (this.from == null) return
    const [a, b] = [this.from, position]
    this.from = null
    if (!(b > a)) return
    // Kept merged and in order.
    const out: [number, number][] = []
    let [lo, hi] = [a, b]
    for (const [x, y] of this.ranges) {
      if (y < lo) out.push([x, y])
      else if (x > hi) {
        out.push([lo, hi])
        ;[lo, hi] = [x, y]
      } else {
        lo = Math.min(lo, x)
        hi = Math.max(hi, y)
      }
    }
    out.push([lo, hi])
    this.ranges = out.sort((p, q) => p[0] - q[0])
  }
  /** Seconds heard so far; `now` is where the sound is if it is coming out. */
  total(now?: number): number {
    let sum = 0
    let [lo, hi] = this.from != null && now != null && now > this.from ? [this.from, now] : [NaN, NaN]
    for (const [x, y] of this.ranges) {
      if (!Number.isNaN(lo) && !(y < lo || x > hi)) {
        lo = Math.min(lo, x)
        hi = Math.max(hi, y)
      } else sum += y - x
    }
    if (!Number.isNaN(lo)) sum += hi - lo
    return sum
  }
}

interface ElementVoice {
  kind: 'element'
  track: EngineTrack
  el: HTMLAudioElement
}
interface BufferVoice {
  kind: 'buffer'
  track: EngineTrack
  buffer: AudioBuffer
  gain: GainNode
  node: AudioBufferSourceNode | null
  /** The audio-clock time at which position 0 of the song is (or would have been) played. */
  zero: number
  /** Where it stands while it is not sounding. */
  at: number
}
type Voice = ElementVoice | BufferVoice

/** Songs longer than this are not decoded into memory, whatever they would take. */
export const EXACT_MAX_SECONDS = 15 * 60
/**
 * The most memory one decoded song may take. Decoded sound is 8 bytes a moment for
 * stereo at the rate of the sound output (not the file's): 23 MB a minute at 48 kHz, 46
 * at 96 kHz. Three songs are kept, so this bounds the player at about 750 MB.
 */
export const EXACT_MAX_BYTES = 256 * 1024 * 1024

/** The longest song (seconds) that is decoded for an output of `rate` samples a second. */
export function exactSeconds(rate: number): number {
  return Math.min(EXACT_MAX_SECONDS, EXACT_MAX_BYTES / (Math.max(8000, rate) * 2 * 4))
}
/** How long before an element's end the buffer that follows it is put on the clock. */
const HANDOVER_SECONDS = 0.35
/** Decoded songs kept: the one playing, the next, and the one before (Previous). */
const BUFFERS_KEPT = 3

const dbToGain = (db: number) => Math.pow(10, db / 20)

type AudioContextClass = typeof AudioContext
function contextClass(): AudioContextClass | null {
  const w = globalThis as { AudioContext?: AudioContextClass; webkitAudioContext?: AudioContextClass }
  return w.AudioContext ?? w.webkitAudioContext ?? null
}

export class Engine {
  private listeners = new Set<(e: EngineEvent) => void>()
  private ctx: AudioContext | null = null
  private master: GainNode | null = null
  private analyserNode: AnalyserNode | null = null
  private noContext = false
  private elements: HTMLAudioElement[] = []
  private elementGain = new Map<HTMLAudioElement, GainNode>()
  /** Elements the player itself is about to pause: their next 'pause' is not news. */
  private ownPause = new WeakSet<HTMLAudioElement>()
  private current: Voice | null = null
  private next: Voice | null = null
  private nextTrack: EngineTrack | null = null
  /** The next buffer is on the clock to start when the current one ends. */
  private joined = false
  private wanted = false
  private volume = 1
  private muted = false
  private buffers = new Map<string, Promise<AudioBuffer>>()
  private ticker: ReturnType<typeof setInterval> | undefined
  private heardNow = new Heard()
  /** Joins made, for the page that shows how the player is doing (and the browser check). */
  readonly stats = { exactJoins: 0, clockJoins: 0, plainJoins: 0 }

  on(l: (e: EngineEvent) => void) {
    this.listeners.add(l)
    return () => {
      this.listeners.delete(l)
    }
  }
  private emit(e: EngineEvent) {
    this.listeners.forEach((l) => l(e))
  }

  // ---- the audio graph ----

  /**
   * Makes the audio graph on the first press of Play (a browser allows it then). Without
   * Web Audio everything still plays, through plain elements.
   */
  private graph(): AudioContext | null {
    if (this.ctx || this.noContext) return this.ctx
    const Ctx = contextClass()
    if (!Ctx) {
      this.noContext = true
      return null
    }
    try {
      const ctx = new Ctx({ latencyHint: 'playback' })
      const master = ctx.createGain()
      const analyser = ctx.createAnalyser()
      analyser.fftSize = 4096
      analyser.smoothingTimeConstant = 0.8
      master.connect(analyser)
      analyser.connect(ctx.destination)
      this.ctx = ctx
      this.master = master
      this.analyserNode = analyser
      for (const el of this.elements) this.wire(el)
      this.applyVolume()
    } catch {
      this.noContext = true
    }
    return this.ctx
  }

  private wire(el: HTMLAudioElement) {
    if (!this.ctx || !this.master || this.elementGain.has(el)) return
    try {
      const gain = this.ctx.createGain()
      this.ctx.createMediaElementSource(el).connect(gain)
      gain.connect(this.master)
      this.elementGain.set(el, gain)
    } catch {
      /* this element goes on playing by itself */
    }
  }

  /** The analyser the visualizer reads (null until something has played, or without Web Audio). */
  analyser(): AnalyserNode | null {
    return this.analyserNode
  }
  /** The rate sound leaves the browser at (what the system's output is set to), once known. */
  outputRate(): number | null {
    return this.ctx?.sampleRate ?? null
  }

  private applyVolume() {
    const v = this.muted ? 0 : this.volume
    if (this.master && this.ctx) this.master.gain.setTargetAtTime(v, this.ctx.currentTime, 0.015)
    for (const el of this.elements) {
      const through = this.elementGain.has(el)
      // Through the graph the element stays at full level and the graph does the rest.
      // By itself, the element's own volume is all there is: it can turn a song down for
      // levelling but not up.
      const own = this.current?.kind === 'element' && this.current.el === el ? this.current.track.gainDb : this.next?.kind === 'element' && this.next.el === el ? this.next.track.gainDb : 0
      el.volume = through ? 1 : Math.max(0, Math.min(1, this.volume * Math.min(1, dbToGain(own))))
      el.muted = through ? false : this.muted
    }
  }

  setVolume(volume: number, muted: boolean) {
    this.volume = volume
    this.muted = muted
    this.applyVolume()
  }

  private setVoiceGain(v: Voice) {
    if (v.kind === 'buffer') v.gain.gain.value = dbToGain(v.track.gainDb)
    else {
      const g = this.elementGain.get(v.el)
      if (g) g.gain.value = dbToGain(v.track.gainDb)
    }
    this.applyVolume()
  }

  /** Levelling changed for the song playing (the setting, or what its neighbours are). */
  setGain(gainDb: number) {
    if (!this.current) return
    this.current.track = { ...this.current.track, gainDb }
    this.setVoiceGain(this.current)
  }

  // ---- elements ----

  private element(): HTMLAudioElement {
    const busy = new Set<HTMLAudioElement>()
    if (this.current?.kind === 'element') busy.add(this.current.el)
    if (this.next?.kind === 'element') busy.add(this.next.el)
    let el = this.elements.find((e) => !busy.has(e))
    if (!el) {
      el = new Audio()
      el.preload = 'auto'
      this.listen(el)
      this.elements.push(el)
      this.wire(el)
    }
    return el
  }

  private isCurrent(el: HTMLAudioElement) {
    return this.current?.kind === 'element' && this.current.el === el
  }

  private listen(el: HTMLAudioElement) {
    el.addEventListener('timeupdate', () => this.isCurrent(el) && this.onElementTime(el))
    el.addEventListener('durationchange', () => this.isCurrent(el) && this.emit({ type: 'time' }))
    el.addEventListener('playing', () => {
      if (!this.isCurrent(el)) return
      this.heardNow.open(el.currentTime)
      this.emit({ type: 'playing' })
    })
    el.addEventListener('waiting', () => {
      if (!this.isCurrent(el)) return
      this.heardNow.close(el.currentTime)
      if (this.wanted) this.emit({ type: 'waiting' })
    })
    el.addEventListener('ended', () => {
      if (!this.isCurrent(el)) return
      this.heardNow.close(this.duration())
      this.finished(false)
    })
    el.addEventListener('error', () => {
      if (this.isCurrent(el)) this.emit({ type: 'error' })
      else if (this.next?.kind === 'element' && this.next.el === el) this.next = null // loaded again when its turn comes
    })
    el.addEventListener('pause', () => {
      if (this.ownPause.delete(el) || !this.isCurrent(el) || el.ended) return
      this.heardNow.close(el.currentTime)
      if (this.wanted) {
        this.wanted = false
        this.emit({ type: 'paused' })
      }
    })
    el.addEventListener('play', () => {
      if (!this.isCurrent(el) || this.wanted) return
      this.wanted = true
      this.emit({ type: 'resumed' })
    })
  }

  /** Pauses an element as the player's own doing. */
  private quiet(el: HTMLAudioElement) {
    if (el.paused) return
    this.ownPause.add(el)
    el.pause()
  }

  private startElement(el: HTMLAudioElement) {
    const p = el.play()
    if (p && typeof p.catch === 'function')
      p.catch((e: unknown) => {
        if (!this.isCurrent(el)) return
        if ((e as { name?: string } | null)?.name === 'NotAllowedError') {
          this.wanted = false
          this.emit({ type: 'blocked' })
        }
      })
  }

  private onElementTime(el: HTMLAudioElement) {
    this.emit({ type: 'time' })
    // A buffer that follows an element is put on the audio clock from the element's own
    // time, just before the end: the element's "ended" comes too late to join cleanly.
    const next = this.next
    if (!this.wanted || !this.ctx || next?.kind !== 'buffer' || this.joined || !Number.isFinite(el.duration)) return
    const left = el.duration - el.currentTime
    if (left > 0 && left <= HANDOVER_SECONDS) {
      this.startBuffer(next, 0, this.ctx.currentTime + left)
      this.joined = true
    }
  }

  // ---- buffers ----

  /** Whether a song can be decoded into memory here. */
  canDecode(track: EngineTrack): boolean {
    if (!track.exact || this.noContext || !contextClass() || typeof fetch !== 'function') return false
    // Before the sound output is open its rate is not known: 48 kHz is the usual one.
    if (!(track.duration > 0) || track.duration > exactSeconds(this.ctx?.sampleRate ?? 48000)) return false
    const memory = (navigator as { deviceMemory?: number }).deviceMemory
    return memory == null || memory >= 4
  }

  private decode(track: EngineTrack): Promise<AudioBuffer> {
    const had = this.buffers.get(track.url)
    if (had) {
      this.buffers.delete(track.url) // most recently used last
      this.buffers.set(track.url, had)
      return had
    }
    const ctx = this.graph()
    if (!ctx) return Promise.reject(new Error('no audio context'))
    const made = fetch(track.url, { credentials: 'same-origin' })
      .then((r) => {
        if (!r.ok) throw new Error('HTTP ' + r.status)
        return r.arrayBuffer()
      })
      .then((bytes) => ctx.decodeAudioData(bytes))
    this.buffers.set(track.url, made)
    made.then(
      (buffer) => this.buffers.get(track.url) === made && this.readyBuffers.set(track.url, buffer),
      () => this.buffers.get(track.url) === made && this.buffers.delete(track.url),
    )
    for (const url of this.buffers.keys()) {
      if (this.buffers.size <= BUFFERS_KEPT) break
      if (url === this.current?.track.url || url === this.nextTrack?.url || url === track.url) continue
      this.buffers.delete(url)
      this.readyBuffers.delete(url)
    }
    return made
  }

  private bufferVoice(track: EngineTrack, buffer: AudioBuffer): BufferVoice {
    const gain = this.ctx!.createGain()
    gain.connect(this.master!)
    return { kind: 'buffer', track, buffer, gain, node: null, zero: 0, at: 0 }
  }

  /** Puts a buffer on the audio clock: position `offset` of it sounds at time `when`. */
  private startBuffer(v: BufferVoice, offset: number, when: number) {
    const ctx = this.ctx!
    this.stopBuffer(v)
    const node = ctx.createBufferSource()
    node.buffer = v.buffer
    node.connect(v.gain)
    v.gain.gain.value = dbToGain(v.track.gainDb)
    node.onended = () => {
      if (v.node !== node) return // stopped, or started again elsewhere
      v.node = null
      v.at = v.buffer.duration
      if (this.current === v) {
        this.heardNow.close(v.buffer.duration)
        this.finished(true)
      }
    }
    v.node = node
    v.zero = when - offset
    v.at = offset
    node.start(Math.max(when, ctx.currentTime), Math.max(0, Math.min(offset, v.buffer.duration)))
  }

  private stopBuffer(v: BufferVoice) {
    const node = v.node
    if (!node) return
    v.at = this.positionOf(v)
    v.node = null
    node.onended = null
    try {
      node.stop()
    } catch {
      /* never started */
    }
    node.disconnect()
  }

  private positionOf(v: Voice): number {
    if (v.kind === 'element') return v.el.currentTime
    if (!v.node || !this.ctx) return v.at
    return Math.max(0, Math.min(v.buffer.duration, this.ctx.currentTime - v.zero))
  }

  // ---- what plays ----

  position(): number {
    return this.current ? this.positionOf(this.current) : 0
  }

  duration(): number {
    const v = this.current
    if (!v) return 0
    if (v.kind === 'buffer') return v.buffer.duration
    return Number.isFinite(v.el.duration) && v.el.duration > 0 ? v.el.duration : v.track.duration
  }

  /** Seconds of the song playing that were actually heard, each part once. */
  heard(): number {
    return this.heardNow.total(this.wanted && this.sounding() ? this.position() : undefined)
  }

  private sounding(): boolean {
    const v = this.current
    if (!v) return false
    return v.kind === 'buffer' ? v.node != null : !v.el.paused && !v.el.ended
  }

  /** The kind of voice the song playing has (for the page that shows how it is played). */
  voice(): 'element' | 'buffer' | null {
    return this.current?.kind ?? null
  }

  private tick(on: boolean) {
    if (on === (this.ticker != null)) return
    if (on) this.ticker = setInterval(() => this.current?.kind === 'buffer' && this.current.node && this.emit({ type: 'time' }), 250)
    else {
      clearInterval(this.ticker)
      this.ticker = undefined
    }
  }

  private release(v: Voice | null) {
    if (!v) return
    if (v.kind === 'buffer') {
      this.stopBuffer(v)
      v.gain.disconnect()
    } else this.quiet(v.el)
  }

  /**
   * Plays `track` from `startAt` (or holds it there, paused). When it is the song that was
   * made ready as next, that voice is used.
   */
  load(track: EngineTrack, play: boolean, startAt = 0) {
    if (play) this.graph()
    void this.ctx?.resume?.()
    this.unjoin()
    const old = this.current
    if (old) this.heardNow.close(this.positionOf(old))
    this.heardNow = new Heard()
    this.wanted = play
    let voice: Voice | null = null
    if (this.next && this.nextTrack?.url === track.url) {
      voice = this.next
      voice.track = track
      this.next = null
      this.nextTrack = null
    }
    this.current = null
    this.release(old)
    if (!voice) {
      this.setNext(null)
      const el = this.element()
      this.quiet(el)
      if (!el.src.endsWith(track.url) || el.error) {
        el.src = track.url
        el.load()
      }
      voice = { kind: 'element', track, el }
      // A song that joins its neighbours exactly is decoded as well, to take over from
      // the element at the next seek or pause (never in the middle of the sound).
      if (this.canDecode(track) && this.graph()) void this.decode(track).catch(() => undefined)
    }
    this.current = voice
    this.setVoiceGain(voice)
    if (voice.kind === 'element') {
      try {
        if (startAt > 0 || voice.el.currentTime > 0) voice.el.currentTime = startAt
      } catch {
        /* not seekable yet: it starts from the beginning */
      }
      if (play) this.startElement(voice.el)
    } else {
      voice.at = startAt
      if (play) {
        this.startBuffer(voice, startAt, this.ctx!.currentTime)
        this.heardNow.open(startAt)
        this.emit({ type: 'playing' })
      }
    }
    this.tick(play)
    this.emit({ type: 'time' })
  }

  /**
   * Says what follows the song playing (null: nothing). It is made ready: decoded when an
   * exact join is wanted and possible, else loaded in a second element.
   */
  setNext(track: EngineTrack | null) {
    if (track && this.nextTrack && track.url === this.nextTrack.url && track.exact === this.nextTrack.exact) {
      this.nextTrack = track
      if (this.next) {
        this.next.track = track
        if (!this.joined) this.setVoiceGain(this.next)
      }
      return
    }
    this.unjoin()
    this.release(this.next)
    this.next = null
    this.nextTrack = track
    if (!track) return
    // The player says what follows again and again while a song plays (each time with a
    // new object for the same song): what is still wanted is told by the song's address,
    // and the newest telling of it is the one used (its level may have changed).
    const { url } = track
    const wanted = () => (this.nextTrack?.url === url && !this.next ? this.nextTrack : null)
    const asElement = () => {
      const now = wanted()
      if (!now) return
      const el = this.element()
      this.quiet(el)
      el.src = url
      el.load()
      this.next = { kind: 'element', track: now, el }
    }
    if (this.canDecode(track) && this.graph()) {
      this.decode(track).then((buffer) => {
        const now = wanted()
        if (!now) return
        this.next = this.bufferVoice(now, buffer)
        this.join()
      }, asElement)
    } else asElement()
  }

  /** Puts the next buffer on the clock at the exact end of the buffer playing. */
  private join() {
    const cur = this.current
    const next = this.next
    if (this.joined || !this.wanted || !this.ctx || cur?.kind !== 'buffer' || next?.kind !== 'buffer' || !cur.node) return
    this.startBuffer(next, 0, cur.zero + cur.buffer.duration)
    this.joined = true
  }

  /** Takes the next buffer off the clock (the song playing was paused, moved or replaced). */
  private unjoin() {
    if (!this.joined) return
    this.joined = false
    if (this.next?.kind === 'buffer') this.stopBuffer(this.next)
  }

  /** The song playing came to its end. */
  private finished(onClock: boolean) {
    const done = this.current
    const next = this.next
    if (!next) {
      if (this.nextTrack) {
        // Something follows and is not ready (still being fetched): it plays as soon as
        // it can, through an element.
        const track = this.nextTrack
        this.nextTrack = null
        this.stats.plainJoins++
        this.load(track, true)
        this.emit({ type: 'advanced', exact: false })
        return
      }
      this.wanted = false
      this.tick(false)
      this.emit({ type: 'ended' })
      return
    }
    const wasJoined = this.joined
    this.joined = false
    this.next = null
    this.nextTrack = null
    this.current = next
    this.heardNow = new Heard()
    if (done && done !== next) {
      if (done.kind === 'buffer') done.gain.disconnect()
      else this.quiet(done.el)
    }
    if (next.kind === 'buffer') {
      if (!wasJoined) this.startBuffer(next, 0, this.ctx!.currentTime)
      this.heardNow.open(wasJoined ? Math.max(0, this.positionOf(next)) : 0)
      if (wasJoined && onClock) this.stats.exactJoins++
      else if (wasJoined) this.stats.clockJoins++
      else this.stats.plainJoins++
      this.emit({ type: 'playing' })
    } else {
      this.stats.plainJoins++
      this.setVoiceGain(next)
      if (next.el.currentTime > 0) next.el.currentTime = 0
      this.startElement(next.el)
    }
    this.tick(true)
    this.emit({ type: 'advanced', exact: wasJoined && onClock })
  }

  play() {
    const v = this.current
    if (!v || this.wanted) return
    this.graph()
    void this.ctx?.resume?.()
    this.wanted = true
    // A pause is a break in the sound anyway: a song that was decoded meanwhile is played
    // from memory from here on, so that its end can be joined exactly.
    const up = this.upgraded(v)
    if (up.kind === 'buffer') {
      this.startBuffer(up, up.at, this.ctx!.currentTime)
      this.heardNow.open(up.at)
      this.emit({ type: 'playing' })
      this.join()
    } else this.startElement(up.el)
    this.tick(true)
  }

  pause() {
    const v = this.current
    if (!v || !this.wanted) return
    this.wanted = false
    this.unjoin()
    this.heardNow.close(this.positionOf(v))
    if (v.kind === 'buffer') this.stopBuffer(v)
    else this.quiet(v.el)
    this.tick(false)
  }

  seek(seconds: number) {
    const v = this.current
    if (!v) return
    const to = Math.max(0, Math.min(seconds, this.duration() || seconds))
    this.unjoin()
    this.heardNow.close(this.positionOf(v))
    const up = this.upgraded(v, to)
    if (up.kind === 'buffer') {
      up.at = to
      if (this.wanted) {
        this.startBuffer(up, to, this.ctx!.currentTime)
        this.heardNow.open(to)
        this.join()
      }
    } else {
      try {
        up.el.currentTime = to
      } catch {
        /* nothing loaded to seek in */
      }
      if (this.wanted && !up.el.paused) this.heardNow.open(to)
    }
    this.emit({ type: 'time' })
  }

  /**
   * The voice to go on with at a break in the sound: the decoded buffer when the song has
   * one by now (ready at once: a decode still running is not waited for), else the voice
   * it has.
   */
  private upgraded(v: Voice, at = this.positionOf(v)): Voice {
    const ready = this.readyBuffers.get(v.track.url)
    if (v.kind !== 'element' || !ready || !this.ctx || !this.canDecode(v.track)) return v
    this.quiet(v.el)
    const voice = this.bufferVoice(v.track, ready)
    voice.at = at
    this.current = voice
    return voice
  }

  /** Decoded buffers that have arrived, by address (`buffers` holds the promises). */
  private readyBuffers = new Map<string, AudioBuffer>()

  /** Stops everything and forgets what was loaded. */
  clear() {
    this.unjoin()
    if (this.current) this.heardNow.close(this.positionOf(this.current))
    this.release(this.current)
    this.release(this.next)
    this.current = this.next = null
    this.nextTrack = null
    this.wanted = false
    this.heardNow = new Heard()
    for (const el of this.elements) {
      this.quiet(el)
      el.removeAttribute('src')
      el.load()
    }
    this.buffers.clear()
    this.readyBuffers.clear()
    this.tick(false)
  }
}
