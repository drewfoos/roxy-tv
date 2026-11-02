// The slice of the /v1 wire types Roxy Web reads (docs/api/native.md). Unknown fields
// are ignored, so the server can grow without breaking the client.

export interface User {
  id: string
  name: string
  isAdmin?: boolean
}

export interface LoginResponse {
  token: string
  user: User
  deviceId?: string
}

export interface Me {
  id?: string
  name?: string
  isAdmin?: boolean
  apiKey?: boolean
  profileId?: string | null
}

export interface Profile {
  id: string
  name: string
  isDefault?: boolean
  avatarName?: string
  hasAvatar?: boolean
  avatarKey?: string
}

export interface ProfilesResponse {
  profiles: Profile[]
  current?: string | null
}

export interface UserData {
  positionMs: number
  played: boolean
  playCount?: number
  isFavorite: boolean
  lastPlayedAt?: string | null
  audioIndex?: number | null
  subtitleIndex?: number | null
}

export type StreamType = 'video' | 'audio' | 'subtitle'

export interface MediaStream {
  index: number
  type: StreamType
  codec: string
  profile?: string
  bitDepth?: number
  pixFmt?: string
  width?: number
  height?: number
  fps?: number
  rangeType?: string
  channels?: number
  channelLayout?: string
  bitrate?: number
  language?: string
  detectedLanguage?: string
  title?: string
  isDefault?: boolean
  isForced?: boolean
  isExternal?: boolean
  isTextFormat?: boolean
}

export interface Attachment {
  index: number
  filename: string
  mime?: string
}

export interface MediaSegment {
  type: 'intro' | 'outro' | 'recap' | 'preview'
  startMs: number
  endMs: number
  origin?: string
}

export interface MediaSource {
  id: string
  itemId?: string
  container?: string
  size?: number
  durationMs?: number
  isPrimary?: boolean
  streams?: MediaStream[]
  attachments?: Attachment[]
  segments?: MediaSegment[]
}

export type ItemType = 'series' | 'season' | 'episode' | 'movie'

export interface Item {
  id: string
  type: ItemType
  title: string
  sortTitle?: string
  parentId?: string | null
  seriesId?: string | null
  seriesTitle?: string | null
  year?: number | null
  overview?: string | null
  tagline?: string | null
  genres?: string[]
  advisories?: string[]
  communityRating?: number | null
  officialRating?: string | null
  premiered?: string | null
  airDate?: string | null
  status?: string | null
  studio?: string | null
  seasonCount?: number | null
  relatedCount?: number
  indexAired?: number | null
  seasonAired?: number | null
  indexAbsolute?: number | null
  runtimeMs?: number | null
  dateCreated?: string | null
  isVirtual?: boolean
  hasMedia?: boolean
  availability?: 'available' | 'missing' | 'unaired' | string | null
  /** poster | backdrop | logo | thumb | hero -> relative URL; absent key = no image. */
  images?: Record<string, string>
  providerIds?: Record<string, string>
  audioLanguages?: string[]
  isAnime?: boolean
  userData?: UserData | null
  mediaSources?: MediaSource[]
}

export interface ItemsResponse {
  items: Item[]
  total?: number
}

export interface HomeRow {
  key: string
  title?: string
  items: Item[]
}

export interface HomeResponse {
  rows: HomeRow[]
}

export interface TrickplayManifest {
  intervalMs: number
  width: number
  height: number
  columns: number
  rows: number
  count: number
  sheets: number
}

export interface MediaTokenResponse {
  token: string
  expiresAt?: string
}

export interface RelatedEntry {
  relation: string
  provider?: string
  providerId?: string
  title: string
  format?: string | null
  year?: number | null
  episodes?: number | null
  inLibrary?: boolean
  item?: Item | null
  posterUrl?: string | null
}

export interface RelatedResponse {
  related: RelatedEntry[]
  partOf?: Item[]
}

export interface DiscoverItem {
  type: 'tv' | 'movie' | string
  tmdbId: number
  title: string
  year?: number | null
  poster?: string | null
  backdrop?: string | null
  overview?: string | null
  rating?: number | null
  genres?: string[]
  isAnime?: boolean
  /** Not sent by the server: "in the library" is `libraryItemId` being set. */
  inLibrary?: boolean
  libraryItemId?: string | null
  /** pending | sent | failed | approval | declined when someone asked for it. */
  requestStatus?: string | null
  /** What Sonarr / Radarr are doing with the request (absent before it reached them). */
  download?: DownloadStatus | null
}

/**
 * A request's download status from the server's tracker. state: searching | notFound |
 * waiting | queued | downloading | stalled | importing | attention | failed | complete.
 */
export interface DownloadStatus {
  state: string
  progress?: number
  timeLeftSec?: number
  message?: string | null
  have?: number
  wanted?: number
  upcoming?: number
  nextAir?: string | null
  lastSearch?: string | null
  groups?: DownloadGroup[]
  canSearch?: boolean
  canRetry?: boolean
  updatedAt?: string | null
  episodes?: EpisodeDownload[]
  /** waiting: "airing", "release" or "digital" (in cinemas). */
  waitingFor?: string | null
}

export interface DownloadGroup {
  state: string
  label?: string
  count?: number
  progress?: number
  timeLeftSec?: number
  message?: string | null
}

export interface EpisodeDownload {
  season: number
  episode: number
  state: string
  progress?: number
  timeLeftSec?: number
  message?: string | null
  airDate?: string | null
  canSearch?: boolean
  canRetry?: boolean
}

export interface RequestRow {
  id: string
  type: string
  tmdbId: number
  title: string
  year?: number
  userId?: string
  userName?: string
  /** pending | sent | failed | approval | declined */
  status: string
  error?: string
  seasons?: number[]
  episodes?: string[]
  createdAt?: string
}

/** One season on a Discover title page, with what the library holds and whether it is requested. */
export interface DiscoverSeason {
  number: number
  name?: string
  episodes: number
  airDate?: string | null
  poster?: string | null
  have: number
  known?: number
  requested: boolean
  /** TMDB season 0: requested one episode at a time. */
  special?: boolean
}

export interface DiscoverEpisode {
  number: number
  title?: string
  overview?: string | null
  airDate?: string | null
  still?: string | null
  have: boolean
  requested?: boolean
  download?: EpisodeDownload | null
}

/** GET /v1/discover/{type}/{tmdbId}: the title page. */
export interface DiscoverDetail extends DiscoverItem {
  tagline?: string | null
  status?: string | null
  logo?: string | null
  officialRating?: string | null
  seasonCount?: number
  episodes?: number
  runtimeMinutes?: number
  networks?: string[]
  tvdbId?: number
  imdbId?: string | null
  request?: RequestRow | null
  requestsEnabled: boolean
  seasons?: DiscoverSeason[]
}

export interface CastMember {
  name: string
  character?: string
  photo?: string | null
  tmdbId?: number
}

export interface TmdbExtras {
  originalTitle?: string
  firstAired?: string
  lastAired?: string
  status?: string
  episodes?: number
  seasons?: number
  runtimeMinutes?: number
  networks?: string[]
  companies?: string[]
  countries?: string[]
  languages?: string[]
  keywords?: string[]
  cast?: CastMember[]
  homepage?: string
  tagline?: string
}

/** GET /v1/discover/{type}/{tmdbId}/details: "More details" for a title not (necessarily) in the library. */
export interface ItemDetails {
  item: Item
  tmdb?: TmdbExtras | null
  links?: Record<string, string>
  backdrop?: string | null
}

export interface DiscoverRow {
  key: string
  title: string
  items: DiscoverItem[]
}

export interface DiscoverResponse {
  rows: DiscoverRow[]
  requestsEnabled?: Record<string, boolean>
}

export interface LegalDoc {
  doc: string
  title: string
  version: string
  body: string
}

export interface SystemInfo {
  product: string
  id?: string
  name?: string
  version?: string
}

/** GET /v1/setup: `required` while no account exists; `allowed` only from the home network. */
export interface SetupStatus {
  required?: boolean
  allowed?: boolean
  tmdb?: boolean
  serverName?: string
}
