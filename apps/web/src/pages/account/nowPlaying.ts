import { languageName } from '../../api/items'
import type { ActiveOutput, ActiveSession, ActiveTrack, PlayMethod } from '../../api/admin'

/** The words for how a file reaches its device, and the line under them. */
export const METHODS: Record<PlayMethod, { label: string; plain: string }> = {
  direct: { label: 'Direct play', plain: 'The file is sent exactly as it is. The server does no work on it.' },
  remux: { label: 'Remux', plain: 'The video is repackaged without re-encoding.' },
  transcode: { label: 'Transcoding', plain: 'The server is converting the video as it plays.' },
  '': { label: 'Not known', plain: 'No stream request has been seen for this since the server last started.' },
}

const CODECS: Record<string, string> = {
  hevc: 'HEVC',
  h264: 'H.264',
  av1: 'AV1',
  vp9: 'VP9',
  vc1: 'VC-1',
  mpeg2video: 'MPEG-2',
  mpeg4: 'MPEG-4',
  aac: 'AAC',
  ac3: 'AC3',
  eac3: 'E-AC3',
  dts: 'DTS',
  truehd: 'TrueHD',
  flac: 'FLAC',
  opus: 'Opus',
  mp3: 'MP3',
  vorbis: 'Vorbis',
  ass: 'ASS',
  ssa: 'SSA',
  subrip: 'SRT',
  webvtt: 'WebVTT',
  mov_text: 'Text',
  hdmv_pgs_subtitle: 'PGS',
  dvd_subtitle: 'VobSub',
}

export function codecName(codec: string): string {
  return CODECS[codec.toLowerCase()] ?? codec.toUpperCase()
}

/**
 * "1080p" by the picture's width when it is known: a film cropped to 1920x804 is still a
 * 1080p file, and by its height alone it would read "804p".
 */
export function resolution(width?: number, height?: number): string | null {
  const w = width ?? 0
  if (w >= 3800) return '2160p'
  if (w >= 2500) return '1440p'
  if (w >= 1900) return '1080p'
  if (w >= 1260) return '720p'
  return height ? `${height}p` : null
}

export function channelsLabel(n?: number): string | null {
  if (!n) return null
  return n === 1 ? 'mono' : n === 2 ? 'stereo' : `${n - 1}.1`
}

export function bitrateLabel(kbps?: number): string | null {
  if (!kbps) return null
  return kbps >= 1000 ? `${(kbps / 1000).toFixed(1).replace(/\.0$/, '')} Mbps` : `${kbps} kbps`
}

/** "HEVC · 1080p · 10-bit · HDR10": the file's video. */
export function videoText(t: ActiveTrack): string {
  const range = t.range && t.range !== 'SDR' ? t.range : null
  return [codecName(t.codec), resolution(t.width, t.height), t.bitDepth && t.bitDepth > 8 ? `${t.bitDepth}-bit` : null, range].filter(Boolean).join(' · ')
}

/** "English · E-AC3 5.1": the audio track playing. */
export function audioText(t: ActiveTrack): string {
  const what = [codecName(t.codec), channelsLabel(t.channels)].filter(Boolean).join(' ')
  return [t.language ? languageName(t.language) : null, what].filter(Boolean).join(' · ')
}

/** "English · Dialogue · ASS": the subtitle track chosen. */
export function subtitleText(t: ActiveTrack): string {
  const title = t.title && !/^(und|unknown)$/i.test(t.title) ? t.title : null
  return [t.language ? languageName(t.language) : null, title, t.sdh ? 'SDH' : null, codecName(t.codec)].filter(Boolean).join(' · ')
}

/** "H.264 · 720p · 4 Mbps", or "AAC 5.1": what a converted track is sent as. */
export function outputText(o: ActiveOutput): string {
  if (o.height || o.bitrateK) return [codecName(o.codec), o.height ? `${o.height}p` : null, bitrateLabel(o.bitrateK)].filter(Boolean).join(' · ')
  return [codecName(o.codec), channelsLabel(o.channels)].filter(Boolean).join(' ')
}

/**
 * Where a session is by now: a playing one has moved on since it reported (`ageMs` when
 * the answer was made, plus the time since the page got it). Never past the end.
 */
export function livePositionMs(s: Pick<ActiveSession, 'state' | 'positionMs' | 'durationMs' | 'ageMs'>, sinceAnswerMs: number): number {
  const at = s.state === 'playing' ? s.positionMs + s.ageMs + Math.max(sinceAnswerMs, 0) : s.positionMs
  return s.durationMs > 0 ? Math.min(at, s.durationMs) : at
}

/** "12 min left", "1 h 5 min left", or null at the end or with no length. */
export function timeLeft(positionMs: number, durationMs: number): string | null {
  if (durationMs <= 0) return null
  const min = Math.ceil((durationMs - positionMs) / 60000)
  if (min <= 0) return null
  return min >= 60 ? `${Math.floor(min / 60)} h ${min % 60} min left` : `${min} min left`
}

/** "2 playing", "1 paused", "2 playing, 1 paused": the line over the cards. */
export function tally(sessions: Pick<ActiveSession, 'state'>[]): string {
  const playing = sessions.filter((s) => s.state === 'playing').length
  const paused = sessions.length - playing
  return [playing ? `${playing} playing` : null, paused ? `${paused} paused` : null].filter(Boolean).join(', ')
}
