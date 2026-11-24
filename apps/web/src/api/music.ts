// Music: /v1/music (docs/api/music.md). Songs are read from the music folders by their
// tags; an album is listed once two of its songs are there; plays and favourites belong
// to the profile.
import { call } from './client'

// Read from the client when a call is made, not when this file loads: a test that stands
// in for the client need not know about music to load a page that links to it.
const get = <T>(path: string) => call.get<T>(path)
const post = <T>(path: string, body?: unknown) => call.post<T>(path, body)
const del = <T>(path: string) => call.del<T>(path)
const q = (params: Record<string, string | number | boolean | undefined>) => call.q(params)

export interface ArtistRef {
  id: string
  name: string
}

export interface MusicTrack {
  id: string
  title: string
  /** The artist as the file is tagged ("A feat. B"). */
  artist: string
  artistId: string
  /** Everyone credited, the main artist first. */
  artists?: ArtistRef[]
  albumId?: string
  album?: string
  albumArtist?: string
  disc?: number
  track?: number
  year?: number
  durationMs: number
  container: string
  codec: string
  /** Bits per second. */
  bitrate?: number
  sampleRate?: number
  bitDepth?: number
  channels?: number
  lossless: boolean
  size: number
  /** The picture to show (the file's own, else its album's): a hash for artUrl. */
  art?: string
  /** Where lyrics come from ("lrc", "txt", "embedded"); absent when there are none. */
  lyrics?: string
  gainTrack?: number
  gainAlbum?: number
  favorite: boolean
  playCount: number
  lastPlayedAt?: string
  addedAt: string
}

export interface MusicAlbum {
  id: string
  title: string
  artistId: string
  artist: string
  year?: number
  art?: string
  trackCount: number
  durationMs: number
  addedAt: string
  favorite: boolean
}

export interface MusicArtist {
  id: string
  name: string
  art?: string
  trackCount: number
  albumCount: number
  favorite: boolean
}

export interface MusicCounts {
  tracks: number
  albums: number
  artists: number
}

/** One tile of "recently added": an album that came in, or a song that stands alone. */
export interface MusicEntry {
  album?: MusicAlbum
  track?: MusicTrack
}

export interface MusicHome {
  counts: MusicCounts
  scanning: boolean
  recentlyAdded?: MusicEntry[]
  recentlyPlayed?: MusicTrack[]
  mostPlayed?: MusicTrack[]
}

export interface LyricLine {
  /** When the line starts, in milliseconds (0 on every line of unsynced lyrics). */
  ms: number
  text: string
}

export interface Lyrics {
  source: string
  synced: boolean
  lines: LyricLine[]
}

export interface MusicFolder {
  id: number
  name: string
  root: string
  reachable: boolean
  tracks: number
}

export interface MusicFolderResult {
  folderId: number
  name: string
  files: number
  added: number
  updated: number
  moved: number
  missing: number
  removed: number
  failed: number
  leftAlone?: string
  problems?: string[]
}

export interface MusicScanStatus {
  scanning: boolean
  startedAt?: string
  finishedAt?: string
  done: number
  total: number
  folders?: MusicFolderResult[]
  error?: string
}

export type TrackSort = 'title' | 'artist' | 'album' | 'added' | 'plays' | 'recent' | 'favorited'

export interface TracksQuery {
  sort?: TrackSort
  q?: string
  artist?: string
  album?: string
  favorite?: boolean
  played?: boolean
  ids?: string
  limit?: number
  offset?: number
}

export type FavoriteKind = 'track' | 'album' | 'artist'

export const music = {
  home: () => get<MusicHome>('/v1/music'),
  tracks: (p: TracksQuery = {}) => get<{ total: number; tracks: MusicTrack[] }>('/v1/music/tracks' + q({ ...p })),
  track: (id: string) => get<MusicTrack>('/v1/music/tracks/' + encodeURIComponent(id)),
  artists: (p: { q?: string; guests?: boolean; favorite?: boolean } = {}) => get<{ total: number; artists: MusicArtist[] }>('/v1/music/artists' + q({ ...p })),
  artist: (id: string) => get<{ artist: MusicArtist; tracks: MusicTrack[]; albums: MusicAlbum[] }>('/v1/music/artists/' + encodeURIComponent(id)),
  albums: (p: { q?: string; artist?: string; sort?: 'title' | 'added' | 'year' | 'artist'; favorite?: boolean } = {}) =>
    get<{ total: number; albums: MusicAlbum[] }>('/v1/music/albums' + q({ ...p })),
  album: (id: string) => get<{ album: MusicAlbum; tracks: MusicTrack[] }>('/v1/music/albums/' + encodeURIComponent(id)),
  search: (query: string) => get<{ artists: MusicArtist[]; albums: MusicAlbum[]; tracks: MusicTrack[] }>('/v1/music/search' + q({ q: query })),
  lyrics: (id: string) => get<Lyrics>('/v1/music/tracks/' + encodeURIComponent(id) + '/lyrics'),
  favorite: (kind: FavoriteKind, id: string, on: boolean) => (on ? post<void> : del<void>)(`/v1/music/favorites/${kind}/${encodeURIComponent(id)}`),
  /** Counts one listen. Sent once per play, when half the song (or four minutes) was heard. */
  played: (id: string) => post<{ playCount: number }>('/v1/music/tracks/' + encodeURIComponent(id) + '/played'),
  /** The audio file itself (the browser asks for it with its cookie). */
  streamUrl: (id: string) => '/v1/music/tracks/' + encodeURIComponent(id) + '/stream',
  /** A cover's address, or null when there is no picture. Its content never changes. */
  artUrl: (hash: string | undefined, widthPx?: number): string | null => {
    if (!hash) return null
    return '/v1/music/art/' + hash + (widthPx && widthPx > 0 ? '?w=' + Math.round(widthPx) : '')
  },

  // ---- admin ----
  folders: () => get<{ folders: MusicFolder[]; scan: MusicScanStatus }>('/v1/music/folders'),
  addFolder: (path: string, name: string) => post<MusicFolder>('/v1/music/folders', { path, name }),
  removeFolder: (id: number) => del<void>('/v1/music/folders/' + id),
  scan: (force = false) => post<void>('/v1/music/scan', { force }),
}

/** "3:07", "1:02:45". */
export function clock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

/** "42 min", "1 hr 12 min": the length of an album or a queue. */
export function longDuration(ms: number): string {
  const min = Math.max(1, Math.round(ms / 60000))
  if (min < 60) return `${min} min`
  const h = Math.floor(min / 60)
  const rest = min % 60
  return rest ? `${h} hr ${rest} min` : `${h} hr`
}

const CODEC_NAMES: Record<string, string> = {
  mp3: 'MP3',
  flac: 'FLAC',
  alac: 'ALAC',
  aac: 'AAC',
  opus: 'Opus',
  vorbis: 'Vorbis',
  wavpack: 'WavPack',
  ape: 'APE',
  wmav2: 'WMA',
  wmalossless: 'WMA Lossless',
}

/** The codec as people write it ("FLAC", "MP3"; plain PCM is named by its container: "WAV"). */
export function codecName(t: Pick<MusicTrack, 'codec' | 'container'>): string {
  if (t.codec.startsWith('pcm_')) return t.container.toUpperCase()
  if (t.codec.startsWith('dsd_')) return 'DSD'
  return CODEC_NAMES[t.codec] ?? t.codec.toUpperCase()
}

/** kHz without a needless decimal: 44100 -> "44.1", 96000 -> "96". */
function kHz(rate: number): string {
  const k = rate / 1000
  return Number.isInteger(k) ? String(k) : k.toFixed(1)
}

/**
 * How a song is encoded, in the words on the player bar: "FLAC · 24-bit / 96 kHz" for
 * lossless (its resolution is what matters), "MP3 · 320 kbps" for the rest.
 */
export function qualityLabel(t: Pick<MusicTrack, 'codec' | 'container' | 'lossless' | 'bitDepth' | 'sampleRate' | 'bitrate'>): string {
  const name = codecName(t)
  if (t.lossless) {
    const parts: string[] = []
    if (t.bitDepth) parts.push(`${t.bitDepth}-bit`)
    if (t.sampleRate) parts.push(`${kHz(t.sampleRate)} kHz`)
    return parts.length ? `${name} · ${parts.join(' / ')}` : name
  }
  return t.bitrate ? `${name} · ${Math.round(t.bitrate / 1000)} kbps` : name
}

/** Higher than CD quality: more than 16 bits or more than 48 kHz, lossless. */
export function isHiRes(t: Pick<MusicTrack, 'lossless' | 'bitDepth' | 'sampleRate'>): boolean {
  return t.lossless && ((t.bitDepth ?? 0) > 16 || (t.sampleRate ?? 0) > 48000)
}

/** The short mark on a song's row: "Hi-Res", "Lossless", or nothing for ordinary files. */
export function qualityMark(t: Pick<MusicTrack, 'lossless' | 'bitDepth' | 'sampleRate'>): string {
  if (isHiRes(t)) return 'Hi-Res'
  return t.lossless ? 'Lossless' : ''
}
