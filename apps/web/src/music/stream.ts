// What to ask the server for, and how loud to play it: decided here, as plain values.
import { music, type MusicTrack } from '../api/music'
import type { Levelling, Quality } from './prefs'
import type { Queue } from './queue'

/** What this browser can play, by the kind of file ("audio/flac"). */
export type CanPlay = (mime: string) => boolean

const MIME: Record<string, string> = {
  mp3: 'audio/mpeg',
  flac: 'audio/flac',
  aac: 'audio/mp4; codecs="mp4a.40.2"',
  alac: 'audio/mp4; codecs="alac"',
  opus: 'audio/ogg; codecs="opus"',
  vorbis: 'audio/ogg; codecs="vorbis"',
}

/** The type a browser is asked about for a song's own file ('' when no browser plays it). */
export function nativeMime(t: Pick<MusicTrack, 'codec' | 'container'>): string {
  if (t.codec.startsWith('pcm_')) return t.container === 'wav' ? 'audio/wav' : ''
  if (t.codec === 'opus' && t.container !== 'ogg' && t.container !== 'opus') return t.container === 'mka' ? '' : 'audio/webm; codecs="opus"'
  return MIME[t.codec] ?? ''
}

let asked: HTMLAudioElement | null = null
/** The browser's own answer. */
export const browserCanPlay: CanPlay = (mime) => {
  try {
    asked ??= document.createElement('audio')
    return asked.canPlayType(mime) !== ''
  } catch {
    return false
  }
}

export interface Stream {
  url: string
  /** Not the file itself: what it is converted to ("opus", 128). */
  converted?: { codec: 'opus' | 'aac' | 'mp3' | 'flac'; kbps: number }
}

/** The lossy format this browser plays best. */
function lossy(can: CanPlay): 'opus' | 'aac' | 'mp3' {
  if (can(MIME.opus)) return 'opus'
  if (can(MIME.aac)) return 'aac'
  return 'mp3'
}

/**
 * The stream for a song: the file itself when the browser plays it and the quality asked
 * for allows it, else a conversion. A lossy file is never converted to a higher bitrate
 * than it has, and a lossless one that the browser cannot play is converted to FLAC
 * (still lossless) where the browser plays that.
 */
export function chooseStream(t: MusicTrack, quality: Quality, can: CanPlay = browserCanPlay): Stream {
  const mime = nativeMime(t)
  const plays = mime !== '' && can(mime)
  const original: Stream = { url: music.streamUrl(t.id) }
  if (quality === 'original') {
    if (plays) return original
    if (t.lossless && can(MIME.flac)) return { url: music.streamUrl(t.id, { format: 'flac' }), converted: { codec: 'flac', kbps: 0 } }
    const codec = lossy(can)
    const kbps = codec === 'opus' ? 256 : 320
    return { url: music.streamUrl(t.id, { format: codec, bitrate: kbps }), converted: { codec, kbps } }
  }
  // A file already at or under what is asked for goes as it is.
  const has = Math.round((t.bitrate ?? 0) / 1000)
  if (plays && !t.lossless && has > 0 && has <= quality * 1.15) return original
  const codec = lossy(can)
  const kbps = codec === 'opus' ? Math.min(quality, 256) : quality
  return { url: music.streamUrl(t.id, { format: codec, bitrate: kbps }), converted: { codec, kbps } }
}

/** The loudness songs are brought to (LUFS): what the streaming services use. */
export const TARGET_LUFS = -14
/** The most a quiet song is turned up, and a loud one down (dB). */
const MAX_BOOST = 9
const MAX_CUT = -18

/** Whether the song at `index` follows or precedes another song of its album in the queue. */
export function inAlbumRun(q: Queue, index: number): boolean {
  const t = q.tracks[index]
  if (!t?.albumId) return false
  const beside = (o: MusicTrack | undefined) => o != null && o.id !== t.id && o.albumId === t.albumId
  return beside(q.tracks[index - 1]) || beside(q.tracks[index + 1])
}

/**
 * Whether the song at `index` runs on into the next one of the queue, or from the one
 * before: neighbours on the same album, in the album's own order. Those are the joins
 * that have to be exact.
 */
export function runsOn(q: Queue, index: number): boolean {
  const t = q.tracks[index]
  if (!t?.albumId) return false
  const follows = (a: MusicTrack | undefined, b: MusicTrack | undefined) =>
    a != null && b != null && a.albumId === b.albumId && (a.disc ?? 0) === (b.disc ?? 0) && (b.track ?? 0) === (a.track ?? 0) + 1 && (a.track ?? 0) > 0
  return follows(q.tracks[index - 1], t) || follows(t, q.tracks[index + 1])
}

/**
 * How much to turn a song up or down (dB) so that songs are as loud as each other.
 * Played among other songs of its album, the album's loudness is used, so the quiet
 * song of an album stays quieter than the loud one. Without a measurement the file's
 * ReplayGain tag is used; without either the song is left alone. A song is never turned
 * up so far that its peaks would clip; as part of its album the room there is to turn it
 * up is the album's (its highest peak), so that every song of the album is turned by the
 * same amount: by each song's own peak the loud ones would be held back and the quiet
 * ones not, and the album's balance would be gone.
 */
export function levelGain(t: MusicTrack, mode: Levelling, amongItsAlbum: boolean): number {
  if (mode === 'off') return 0
  let loudness: number | undefined
  let peak = t.peak
  if (amongItsAlbum && t.albumLoudness != null) {
    loudness = t.albumLoudness
    peak = t.albumPeak ?? t.peak
  } else if (t.loudness != null) loudness = t.loudness
  // ReplayGain says how far a song is from -18 LUFS.
  else if (amongItsAlbum && t.gainAlbum != null) loudness = -18 - t.gainAlbum
  else if (t.gainTrack != null) loudness = -18 - t.gainTrack
  if (loudness == null || loudness <= -70) return 0
  let gain = TARGET_LUFS - loudness
  if (peak != null && gain > -peak) gain = -peak
  return Math.round(Math.max(MAX_CUT, Math.min(MAX_BOOST, gain)) * 10) / 10
}
