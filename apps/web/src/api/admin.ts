// Types for the account and server-settings pages (what the phone's settings screens read).

export interface AccountUser {
  id: string
  name: string
  role: 'admin' | 'requester' | 'basic' | string
  isDisabled?: boolean
  isYou?: boolean
  createdAt?: string
}

/** A device signed in with this login (GET /v1/devices). */
export interface SignedInDevice {
  id: string
  name: string
  client?: string
  lastSeen?: string
  createdAt?: string
  current: boolean
  online: boolean
  /** The address it last came from; shown only on request. */
  ip?: string
  network?: 'home' | 'tailscale' | 'server' | 'internet' | ''
  nowPlaying?: { title?: string } | null
}

export interface PlayerDevice {
  deviceId: string
  name: string
  client?: string
  lastSeen?: string
  nowPlaying?: { itemId?: string; title?: string; positionMs?: number } | null
}

export interface RequestDownload {
  state?: string
  progress?: number
  timeLeftSec?: number
  message?: string
  have?: number
  wanted?: number
  upcoming?: number
  nextAir?: string
  isProblem?: boolean
}

export interface MediaRequest {
  id: string
  type: 'tv' | 'movie' | string
  tmdbId: number
  title: string
  year?: number
  userId?: string
  userName?: string
  status: 'pending' | 'sent' | 'failed' | 'approval' | 'declined' | string
  /** Seasons asked for; empty is the whole series, [-1] means episodes only. */
  seasons?: number[]
  /** Single episodes asked for, as "S0E5" codes. */
  episodes?: string[]
  /** Why it failed to send, or the decline reason. */
  error?: string
  createdAt?: string
  updatedAt?: string
  poster?: string
  download?: RequestDownload | null
}

export interface ManageTitle {
  id: string
  type: string
  title: string
  year?: number
  matched: boolean
  locked?: boolean
  providerIds?: Record<string, string>
  poster?: string
}

export interface ManageStatus {
  scanning: boolean
  lastScanAt?: string | null
  queue: number
  titles: ManageTitle[]
}

export interface LibraryFolder {
  id: string
  name: string
  kind: 'series' | 'movies' | string
  root: string
  animeMode?: boolean
  series?: number
  movies?: number
  files?: number
  reachable?: boolean
}

export interface BrowseDir {
  name: string
  path: string
  videos?: number
  /** Why a scan leaves this folder alone (download folder, extras...); empty when it doesn't. */
  skip?: string
}

export interface BrowseResponse {
  path: string
  parent?: string | null
  dirs: BrowseDir[]
  roots?: BrowseDir[]
}

export interface IdentifyCandidate {
  id: string | number
  title: string
  year?: number
  overview?: string
  poster?: string
  type?: string
}

export interface ArrConfig {
  url: string
  apiKey: string
  rootFolder?: string
  qualityProfileId?: number | null
  animeRootFolder?: string
  animeQualityProfileId?: number | null
  /** Sonarr only: older anime (first aired before 2010, ended). Empty means the anime profile. */
  oldAnimeQualityProfileId?: number | null
  /** Sonarr only: the series type anime is added with. Empty means standard. */
  animeSeriesType?: '' | 'standard' | 'anime'
  tags?: number[]
  animeTags?: number[]
  seasonFolders?: boolean
  minimumAvailability?: string
  searchOnAdd?: boolean
  externalUrl?: string
}

export interface ArrSettings {
  sonarr: ArrConfig
  radarr: ArrConfig
  requestsEnabled?: { tv: boolean; movie: boolean }
}

export interface ArrTest {
  version?: string
  appName?: string
  supported?: boolean
  note?: string
  error?: string
  rootFolders?: { path: string; freeSpace?: number; roxy?: string; roxyMatch?: 'yes' | 'maybe' | '' }[]
  qualityProfiles?: { id: number; name: string }[]
  tags?: { id: number; label: string }[]
}

export interface TranscodingSettings {
  hwAccel: string
  encoder?: string
  options: { id: string; label: string; available: boolean }[]
}

export interface ApiKeyRow {
  id: string
  name: string
  createdAt?: string
}

export interface WebhookEvent {
  id?: string
  source: string
  eventType: string
  title?: string
  result?: string
  at?: string
  receivedAt?: string
}

export interface AvatarCharacter {
  name: string
  series?: string
  image: string
}
