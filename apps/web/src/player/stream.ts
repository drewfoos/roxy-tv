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

/**
 * The quality an episode starts at, from the one saved in Preferences. That one is for
 * saving data, so a file already that small plays as it is: re-encoding it saves nothing
 * (the server never makes the picture taller than the file's). "A few lines taller"
 * counts as that size, as in the apps (0.94). Only for the saved preference: a quality
 * picked in the gear menu is used as picked. [srcHeight] is the file's classHeight.
 */
export function startQuality(pref: 'original' | number, srcHeight: number): 'original' | QualityStep {
  const step = QUALITY_STEPS.find((h) => h === pref)
  if (!step) return 'original'
  return srcHeight > 0 && step >= srcHeight * 0.94 ? 'original' : step
}

/** The sizes the server converts to, largest first, named by height. The apps offer the same ones. */
export const QUALITY_STEPS = [2160, 1440, 1080, 720, 480] as const
export type QualityStep = (typeof QUALITY_STEPS)[number]

/** The video bitrate asked for with each (kbps), as in the apps. */
export const QUALITY_KBPS: Record<QualityStep, number> = { 2160: 40000, 1440: 16000, 1080: 8000, 720: 4000, 480: 1500 }

/**
 * The height that names a picture's size: its own, or, for a picture wider than 16:9, the
 * height a 16:9 picture of its width has. A 3840 x 1604 film is 4K (2160) and a 1920 x 802
 * one 1080p, which is how the server's conversions count them. By height alone they were
 * "1604p" and "802p", and the 4K film was offered nothing between itself and 1080p.
 */
export function classHeight(width: number | undefined, height: number | undefined): number {
  if (!height || !(height > 0)) return 0
  return Math.max(height, width && width > 0 ? Math.round((width * 9) / 16) : 0)
}

/** "4K", "1080p": what a picture of that (class) height is called; a few lines short still counts. */
export function resolutionLabel(height: number): string {
  if (!(height > 0)) return ''
  const std = [4320, 2160, 1440, 1080, 720, 576, 480, 360].find((s) => height >= s * 0.94)
  return std === 4320 ? '8K' : std === 2160 ? '4K' : `${std ?? height}p`
}

/**
 * The conversions the gear menu lists for a file of that class height: only the ones
 * really smaller than the file (all of them when its size is not known).
 */
export function smallerSteps(srcHeight: number): QualityStep[] {
  return QUALITY_STEPS.filter((h) => srcHeight <= 0 || h < srcHeight * 0.94)
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
  const e = streamEnded(mode, playedTo, duration, reopens)
  return e.kind === 'reopen' ? e.at : null
}

/**
 * What a stream that stopped means: the real end (within 10 s of the end, or not a server
 * stream); a lost connection to reopen at `at`; or one lost too often in a row ('lost'),
 * which is still NOT the end. Telling the last two apart only by "no reopening" made a
 * fourth cut in a row count as the end: the episode was marked watched at the header's
 * duration and autoplay moved on.
 */
export type StreamEnd = { kind: 'end' } | { kind: 'reopen'; at: number } | { kind: 'lost'; at: number }

export function streamEnded(mode: Mode, playedTo: number, duration: number, reopens: number): StreamEnd {
  if (mode.kind !== 'server' || duration <= 0) return { kind: 'end' }
  const at = mode.offset + playedTo
  if (at >= duration - 10) return { kind: 'end' }
  return reopens >= MAX_REOPENS ? { kind: 'lost', at } : { kind: 'reopen', at }
}

/** A report's position counts as moved (fresh) past this; a paused frame's clock jitters less. */
export const MOVED_SEC = 0.5
