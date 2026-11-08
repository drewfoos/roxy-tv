// What the player streams, and how it tells a stream that ran out from the real end.
// Kept out of Player.tsx so it can be tested without a browser (stream.test.ts).

/** Codecs every browser decodes; a file whose chosen track is anything else is remuxed. */
export const BROWSER_AUDIO = new Set(['aac', 'opus', 'flac', 'vorbis', 'mp3'])

/** Direct play, or a server stream (remux or transcode) whose clock starts at `offset`. */
export type Mode = { kind: 'direct' } | { kind: 'server'; audio: number | null; offset: number }

export interface AudioTrack {
  index: number
  codec: string
}

/**
 * How to play: the file itself when the quality is the original and the chosen audio
 * track is the file's first and one browsers decode; a remux when only the audio is the
 * problem (another track chosen, or AC3/DTS and the like); a transcode for any other
 * quality.
 */
export function chooseStream(quality: 'original' | number, tracks: AudioTrack[], audio: number | null): 'direct' | 'remux' | 'transcode' {
  if (quality !== 'original') return 'transcode'
  const first = tracks[0]
  const chosen = tracks.find((t) => t.index === audio) ?? first
  if (!chosen || (chosen.index === first?.index && BROWSER_AUDIO.has(chosen.codec.toLowerCase()))) return 'direct'
  return 'remux'
}

/** The part of a <video> element the clock reads. */
export interface VideoClock {
  currentTime: number
  seeking: boolean
  playbackRate: number
}

/**
 * How far a stream really played. When a server stream (transcode, remux) runs out early
 * (the server ended it, restarted, ffmpeg died), Chrome moves the playhead to the
 * duration in the file's header, stretches `buffered` to match, and fires pause then
 * ended: the element itself then says "the end". Only its readings before that jump say
 * where the stream got to. A jump the viewer made is a seek, which resets the clock.
 */
export class StreamClock {
  /** The element's time the stream really played to. */
  playedTo = 0
  private last = 0

  /** A jump larger than normal playback makes between two readings, with no seek. */
  jumped(v: VideoClock): boolean {
    return !v.seeking && Math.abs(v.currentTime - this.last) > 1 + 2 * v.playbackRate
  }

  /**
   * A time update. False when a server stream jumped (ran out): that reading is not
   * where the viewer is, so it is neither shown nor reported. Direct play is never
   * second-guessed (the browser reads the file itself and seeks in it).
   */
  tick(v: VideoClock, server: boolean): boolean {
    const jump = server && this.jumped(v)
    this.last = v.currentTime
    if (jump) return false
    this.playedTo = v.currentTime
    return true
  }

  /** A pause: false when a server stream paused at the jump (not the viewer's position). */
  pauseIsReal(v: VideoClock, server: boolean): boolean {
    return !server || Math.abs(v.currentTime - this.playedTo) <= 1 + 2 * v.playbackRate
  }

  /** A seek done, or a new stream (its clock starts again): playing goes on from here. */
  reset(v: VideoClock): void {
    this.playedTo = v.currentTime
    this.last = v.currentTime
  }
}

/** How many times in a row a stream is reopened before the player gives up. */
export const MAX_REOPENS = 3

/** How much a reopened stream must play, past where it was reopened, to count as recovered. */
export const RECOVERED_AFTER_SEC = 30

/**
 * Reopenings in a row. "In a row" ends only when a reopened stream has really played on
 * (RECOVERED_AFTER_SEC past the point it was reopened at): the browser's 'playing' event,
 * which used to reset the count, fires as soon as a reopened stream starts, so a stream
 * that died again two seconds after each reopening was reopened for ever.
 */
export class ReopenBudget {
  private count = 0
  private from = 0 // the absolute time the last reopening started at

  /** How many reopenings in a row so far (for reopenAt). */
  get used(): number {
    return this.count
  }

  /** A reopening at absolute time `at`. */
  reopened(at: number): void {
    this.count++
    this.from = at
  }

  /** Real playback reached absolute time `t`: far enough past the reopening, it recovered. */
  progress(t: number): void {
    if (this.count > 0 && t >= this.from + RECOVERED_AFTER_SEC) this.count = 0
  }

  /** A new title, a seek or a quality change: a fresh start. */
  reset(): void {
    this.count = 0
    this.from = 0
  }
}

/**
 * Where to reopen a stream that ended or dropped, or null when it is the real end (or
 * not a server stream, or reopened too often in a row): a server stream that stopped
 * more than 10 s before the end of the file is a lost connection.
 */
export function reopenAt(mode: Mode, playedTo: number, duration: number, reopens: number): number | null {
  if (mode.kind !== 'server' || duration <= 0 || reopens >= MAX_REOPENS) return null
  const at = mode.offset + playedTo
  return at < duration - 10 ? at : null
}
