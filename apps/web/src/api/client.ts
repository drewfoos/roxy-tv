// The /v1 client. Same origin as the page: the server serves the app itself, and the
// Vite dev server proxies /v1 to it. Every call carries the device token and the
// profile; a 401 clears the session so the app returns to the login screen.
import { session, deviceName } from './session'
import type { AccountUser, ApiKeyRow, ArrSettings, ArrTest, AvatarCharacter, BrowseResponse, IdentifyCandidate, LibraryFolder, ManageStatus, MediaRequest, PlayerDevice, SignedInDevice, TranscodingSettings, WebhookEvent } from './admin'
import type {
  DiscoverDetail,
  DiscoverEpisode,
  DiscoverItem,
  DiscoverResponse,
  ItemDetails,
  HomeResponse,
  Item,
  ItemsResponse,
  LegalDoc,
  RequestRow,
  LoginResponse,
  Me,
  MediaSource,
  MediaTokenResponse,
  Profile,
  ProfilesResponse,
  RelatedResponse,
  Section,
  SetupStatus,
  SystemInfo,
  TrickplayManifest,
  UserData,
} from './types'

export const APP_VERSION = '0.1.0'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
  get unauthorized() {
    return this.status === 401
  }
  get notFound() {
    return this.status === 404
  }
}

async function request<T>(method: string, path: string, body?: unknown, auth = true, keepalive = false): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const s = session.get()
  if (auth && s) {
    headers.Authorization = 'Bearer ' + s.token
    if (s.profileId) headers['X-Roxy-Profile'] = s.profileId
  }
  // keepalive lets a small request (a progress report) outlive the page that sent it.
  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), keepalive })
  if (!res.ok) {
    let msg = `${res.status} ${method} ${path}`
    try {
      const j = (await res.json()) as { error?: string }
      if (j.error) msg = j.error
    } catch {
      /* not JSON */
    }
    if (res.status === 401 && auth && s) session.set(null)
    throw new ApiError(res.status, msg)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

const get = <T>(path: string, auth = true) => request<T>('GET', path, undefined, auth)
const post = <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {})
const del = <T>(path: string, body?: unknown) => request<T>('DELETE', path, body)
const put = <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {})
const patch = <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {})

function q(params: Record<string, string | number | boolean | undefined>): string {
  const parts: string[] = []
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === false || v === '') continue
    parts.push(`${k}=${encodeURIComponent(v === true ? '1' : String(v))}`)
  }
  return parts.length ? '?' + parts.join('&') : ''
}

export interface ItemsQuery {
  type: string
  parent?: string
  recursive?: boolean
  sort?: 'date_created' | 'title' | 'index'
  desc?: boolean
  limit?: number
  offset?: number
  includeSources?: boolean
  favorites?: boolean
}

/**
 * Pages of /v1/items until the total. Offsets move when the list changes underneath (a scan
 * adds or removes an episode between two pages): one shifts across the boundary and comes
 * twice, or is skipped, and a removal plus an addition leaves the total as it was. Each page
 * carries the server's items change counter (version): when it differs from the first
 * page's, the read starts over (three tries); an item seen twice is kept once. Lists up to
 * the server's cap (20000, 5000 with sources) come in one request: one consistent read.
 */
export async function collectPages(fetch: (limit: number, offset: number) => Promise<ItemsResponse>, page = 20000): Promise<ItemsResponse> {
  let items: Item[] = []
  let total: number | undefined
  let complete = false
  // The version when the server sends it, else (an older server) the total.
  const mark = (r: ItemsResponse) => r.version ?? r.total
  for (let attempt = 0; attempt < 3 && !complete; attempt++) {
    items = []
    let first: number | undefined
    let moved = false
    let stalled = false
    // Advance by what came, not by what was asked: the server may send fewer than asked
    // (its own cap, lower with sources), and a short page is not the end then; the total is.
    for (let offset = 0; ; ) {
      const r = await fetch(page, offset)
      if (offset === 0) {
        first = mark(r)
        total = r.total
      } else if (mark(r) !== first) {
        moved = true
        break
      }
      items.push(...r.items)
      offset += r.items.length
      if (r.total != null ? items.length >= r.total : r.items.length < page) break
      if (r.items.length === 0) {
        // Short of the total and nothing more comes: no progress, not the end.
        stalled = true
        break
      }
    }
    complete = !moved && !stalled
    if (stalled) break // reading again would stall the same way
  }
  const seen = new Set<string>()
  items = items.filter((it) => !seen.has(it.id) && (seen.add(it.id), true))
  if (!complete) console.warn(`items: read ${items.length} of ${total ?? '?'}; the list is incomplete`)
  return { items, total: complete ? items.length : (total ?? items.length), complete }
}

export const api = {
  // ---- server and auth ----
  systemInfo: () => get<SystemInfo>('/v1/system/info', false),
  setupStatus: () => get<SetupStatus>('/v1/setup', false),
  /** First-time setup: makes the admin (only while no account exists, from the home network) and signs this browser in. */
  setupAdmin: (name: string, password: string) =>
    request<LoginResponse>('POST', '/v1/setup/admin', { name, password, client: 'roxy-web', device: deviceName(), deviceId: session.deviceId(), version: APP_VERSION }, false),
  async login(username: string, password: string): Promise<LoginResponse> {
    return request<LoginResponse>('POST', '/v1/auth/login', { username, password, client: 'roxy-web', device: deviceName(), deviceId: session.deviceId(), version: APP_VERSION }, false)
  },
  me: () => get<Me>('/v1/auth/me'),
  // ---- signing this browser in from a phone (the same flow a TV uses) ----
  linkStart: () =>
    request<{ code: string; secret: string; url: string; expiresIn: number }>('POST', '/v1/link/start', { client: 'roxy-web', device: deviceName(), deviceId: session.deviceId(), version: APP_VERSION }, false),
  /** Long-polls up to ~25 s; "done" carries this browser's own token. */
  linkWait: (code: string, secret: string) =>
    request<{ status: 'pending' } | ({ status: 'done' } & LoginResponse)>('GET', '/v1/link/' + encodeURIComponent(code) + '/wait?secret=' + encodeURIComponent(secret), undefined, false),
  linkQrUrl: (code: string, secret: string) => '/v1/link/' + encodeURIComponent(code) + '/qr.png?secret=' + encodeURIComponent(secret),
  // ---- signing a TV in from this device (the TV shows a code as a QR) ----
  /** network: where the device asking is, per the server (absent on older servers). */
  linkInfo: (code: string) => get<{ code: string; client: string; device: string; expiresIn: number; approved: boolean; network?: 'home' | 'tailscale' | 'internet' }>('/v1/link/' + encodeURIComponent(code)),
  linkApprove: (code: string) => post<void>('/v1/link/' + encodeURIComponent(code) + '/approve'),
  logout: () => post<void>('/v1/auth/logout'),
  /**
   * Asks the server for the same-site cookie that lets the browser load images and
   * video on its own (an <img> cannot send the Bearer header). The cookie is only
   * honoured for GET, so every change still needs the token.
   */
  cookie: () => post<void>('/v1/auth/cookie'),

  // ---- profiles ----
  profiles: () => get<ProfilesResponse>('/v1/me/profiles'),
  createProfile: (name: string, avatarUrl?: string, avatarName?: string) => post<Profile>('/v1/me/profiles', { name, avatarUrl, avatarName }),
  profileAvatarUrl: (id: string, widthPx: number, key = '') => `/v1/me/profiles/${id}/avatar?w=${widthPx}&v=${encodeURIComponent(key)}`,
  /** `avatarUrl` "-" clears the picture; undefined leaves it alone. */
  updateProfile: (id: string, body: { name?: string; avatarUrl?: string; avatarName?: string }) => patch<Profile>(`/v1/me/profiles/${id}`, body),
  deleteProfile: (id: string) => del<void>(`/v1/me/profiles/${id}`),
  avatarsSuggested: () => get<{ characters: AvatarCharacter[] }>('/v1/avatars/suggested'),
  avatarsSearch: (query: string) => get<{ characters: AvatarCharacter[] }>('/v1/avatars/search?q=' + encodeURIComponent(query)),

  // ---- account ----
  /** Signs the login out of every other device; 403 = wrong current password, 429 = throttled. */
  changePassword: (current: string, next: string) => post<void>('/v1/me/password', { current, new: next }),
  players: () => get<{ players: PlayerDevice[] }>('/v1/sessions/players'),
  /** Every device signed in with this login, not only the ones connected now. */
  devices: () => get<{ devices: SignedInDevice[] }>('/v1/devices'),
  /** Sign one of this login's other devices out. */
  signOutDevice: (id: string) => del<void>(`/v1/devices/${id}`),

  // ---- admin: users ----
  users: () => get<{ users: AccountUser[] }>('/v1/users'),
  createUser: (name: string, password: string, role: string) => post<AccountUser>('/v1/users', { name, password, role }),
  updateUser: (id: string, body: { name?: string; password?: string; role?: string; disabled?: boolean }) => patch<AccountUser>(`/v1/users/${id}`, body),
  deleteUser: (id: string) => del<void>(`/v1/users/${id}`),

  // ---- admin: requests ----
  requests: () => get<{ requests: MediaRequest[]; waiting: number }>('/v1/requests'),
  approveRequest: (id: string) => post<void>(`/v1/discover/requests/${id}/approve`),
  declineRequest: (id: string, reason?: string) => post<void>(`/v1/discover/requests/${id}/decline`, { reason }),
  retryRequests: () => post<void>('/v1/discover/requests/retry'),

  // ---- admin: library ----
  libraryManage: () => get<ManageStatus>('/v1/library/manage'),
  libraryScan: (mode: 'files' | 'missing' | 'replace') => post<void>('/v1/library/scan', { mode }),
  libraryRefresh: (body: { ids?: string[]; all?: boolean; metadata: boolean; images: boolean; trickplay: boolean; segments: boolean }) => post<void>('/v1/library/refresh', body),
  folders: () => get<{ libraries: LibraryFolder[] }>('/v1/library/folders'),
  addFolder: (body: { path: string; name: string; kind: string; anime: boolean }) => post<LibraryFolder>('/v1/library/folders', body),
  updateFolder: (id: string, body: { name?: string; kind?: string; anime?: boolean }) => patch<LibraryFolder>(`/v1/library/folders/${id}`, body),
  deleteFolder: (id: string) => del<void>(`/v1/library/folders/${id}`),
  acceptEmptyFolder: (id: string) => post<void>(`/v1/library/folders/${id}/accept-empty`),
  browse: (path?: string) => get<BrowseResponse>('/v1/library/browse' + (path ? '?path=' + encodeURIComponent(path) : '')),
  identifySearch: (itemId: string, provider: 'tmdb' | 'anilist', query: string) => get<{ candidates: IdentifyCandidate[] }>(`/v1/items/${itemId}/identify?provider=${provider}&q=${encodeURIComponent(query)}`),
  identifyApply: (itemId: string, provider: 'tmdb' | 'anilist', id: string | number) => post<void>(`/v1/items/${itemId}/identify`, { provider, id: String(id) }),
  putMetadata: (itemId: string, body: { title?: string; year?: number | null; overview?: string; genres?: string[]; studio?: string; premiered?: string; locked?: boolean }) => put<void>(`/v1/items/${itemId}/metadata`, body),

  // ---- admin: integrations ----
  arrSettings: () => get<ArrSettings>('/v1/settings/arr'),
  saveArrSettings: (body: ArrSettings) => put<ArrSettings>('/v1/settings/arr', body),
  testArr: (kind: 'sonarr' | 'radarr', url: string, apiKey: string) => post<ArrTest>('/v1/settings/arr/test', { kind, url, apiKey }),
  fanart: () => get<{ apiKey: string; enabled: boolean }>('/v1/settings/fanart'),
  saveFanart: (apiKey: string) => put<{ apiKey: string; enabled: boolean }>('/v1/settings/fanart', { apiKey }),
  tmdb: () => get<{ set: boolean; source?: string }>('/v1/settings/tmdb'),
  saveTmdb: (key: string) => put<{ set: boolean }>('/v1/settings/tmdb', { key }),
  transcoding: () => get<TranscodingSettings>('/v1/settings/transcoding'),
  saveTranscoding: (hwAccel: string) => put<TranscodingSettings>('/v1/settings/transcoding', { hwAccel }),
  apiKeys: () => get<{ keys: ApiKeyRow[] }>('/v1/auth/keys'),
  createApiKey: (name: string) => post<{ id: string; name: string; token: string }>('/v1/auth/keys', { name }),
  deleteApiKey: (id: string) => del<void>(`/v1/auth/keys/${id}`),
  webhookEvents: (limit = 30) => get<{ events: WebhookEvent[] }>(`/v1/webhooks/events?limit=${limit}`),

  // ---- library ----
  home: () => get<HomeResponse>('/v1/home'),
  items: (query: ItemsQuery) =>
    get<ItemsResponse>(
      '/v1/items' +
        q({ type: query.type, parent: query.parent, recursive: query.recursive, sort: query.sort, desc: query.desc, limit: query.limit, offset: query.offset, include: query.includeSources ? 'sources' : undefined, favorites: query.favorites }),
    ),
  /**
   * Every item a query matches, page after page: /v1/items sends 100 by default and at
   * most 2000 a request, so a long show's episodes (SpongeBob's 700) came back cut off
   * and "next episode" was picked from the first page only. For whole lists (a show's or
   * a season's episodes), not for rows that want the first few.
   */
  allItems: async (query: Omit<ItemsQuery, 'limit' | 'offset'>): Promise<ItemsResponse> =>
    // The server sends up to 20000 in one request (5000 with sources): one query, one
    // consistent view; pages only past that.
    collectPages((limit, offset) => api.items({ ...query, limit, offset })),
  /** The episode a show's Play means for this profile (a film: itself), worked out by the server. */
  next: (id: string) => get<{ item: Item | null }>(`/v1/items/${id}/next`).then((r) => r.item),
  item: (id: string) => get<Item>(`/v1/items/${id}`),
  source: (id: string) => get<MediaSource>(`/v1/sources/${id}`),
  related: (id: string) => get<RelatedResponse>(`/v1/items/${id}/related`),
  sections: (id: string) => get<{ sections: Section[] }>(`/v1/items/${id}/sections`).then((r) => r.sections),
  /** A film's or show's trailers, featurettes and other extras (each plays like any item). */
  extras: (id: string) => get<{ items: Item[] }>(`/v1/items/${id}/extras`),
  similar: (id: string) => get<{ items: DiscoverItem[] }>(`/v1/items/${id}/similar`),
  /** The Watchlist: the profile's favourites, newest first (the same query as the phone). */
  favorites: (limit = 200) => get<ItemsResponse>(`/v1/items?type=series,movie&recursive=1&favorites=1&sort=date_created&desc=1&limit=${limit}`),
  resume: (limit = 20) => get<ItemsResponse>(`/v1/me/resume?limit=${limit}`),
  history: (limit = 100) => get<ItemsResponse>(`/v1/me/history?limit=${limit}`),

  // ---- watch state ----
  /** subtitleIndex: a track, SUBTITLES_OFF (-1) for off, or undefined to leave the saved choice alone. */
  // ageMs: how long ago (this tab's clock) the position was reached, so a report from a tab
  // left paused cannot undo an episode finished on another device since.
  progress: (itemId: string, positionMs: number, audioIndex?: number, subtitleIndex?: number, ageMs?: number) =>
    request<void>('POST', '/v1/me/progress', { itemId, positionMs, audioIndex, subtitleIndex, ageMs }, true, true),
  setPlayed: (itemId: string, on: boolean) => (on ? post<{ userData: UserData }>(`/v1/me/played/${itemId}`) : del<{ userData: UserData }>(`/v1/me/played/${itemId}`)),
  setFavorite: (itemId: string, on: boolean) => (on ? post<{ userData: UserData }>(`/v1/me/favorite/${itemId}`) : del<{ userData: UserData }>(`/v1/me/favorite/${itemId}`)),

  // ---- media ----
  mediaToken: (sourceId: string) => get<MediaTokenResponse>(`/v1/sources/${sourceId}/token`),
  streamUrl: (sourceId: string, mediaToken: string) => `/v1/stream/${sourceId}?mt=${mediaToken}`,
  /**
   * The source re-encoded to H.264 at a height and bitrate with one browser-safe audio
   * track, timestamps from zero at startSec (web=1).
   */
  transcodeUrl: (sourceId: string, height: number, kbps: number, audioIndex: number | null, startSec: number, mediaToken: string) =>
    `/v1/sources/${sourceId}/transcode?height=${height}&bitrate=${kbps}&start=${startSec.toFixed(3)}&web=1${audioIndex != null ? `&audio=${audioIndex}` : ''}&mt=${mediaToken}`,
  /** The video copied with one audio track (by stream index), timestamps from zero at startSec. */
  remuxUrl: (sourceId: string, audioIndex: number | null, startSec: number, mediaToken: string) =>
    `/v1/sources/${sourceId}/remux?start=${startSec.toFixed(3)}${audioIndex != null ? `&audio=${audioIndex}` : ''}&mt=${mediaToken}`,
  /** Seek-preview sheets: the manifest, or null while the server is still generating them. */
  async trickplay(sourceId: string): Promise<TrickplayManifest | null> {
    const s = session.get()
    const res = await fetch(`/v1/sources/${sourceId}/trickplay`, { headers: s ? { Authorization: 'Bearer ' + s.token } : {} })
    if (res.status !== 200) return null
    return (await res.json()) as TrickplayManifest
  },
  trickplaySheetUrl: (sourceId: string, sheet: number) => `/v1/sources/${sourceId}/trickplay/${sheet}`,
  /** Subtitle and font bytes are fetched with the device token (fetch, not a URL the browser follows). */
  async subtitleText(sourceId: string, index: number): Promise<string> {
    const s = session.get()
    const res = await fetch(`/v1/sources/${sourceId}/subtitles/${index}`, { headers: s ? { Authorization: 'Bearer ' + s.token } : {} })
    if (!res.ok) throw new ApiError(res.status, `subtitle ${index}: ${res.status}`)
    return res.text()
  },
  async attachmentBytes(sourceId: string, index: number): Promise<Uint8Array> {
    const s = session.get()
    const res = await fetch(`/v1/sources/${sourceId}/attachments/${index}`, { headers: s ? { Authorization: 'Bearer ' + s.token } : {} })
    if (!res.ok) throw new ApiError(res.status, `attachment ${index}: ${res.status}`)
    return new Uint8Array(await res.arrayBuffer())
  },

  // ---- legal ----
  legal: (doc: 'terms' | 'privacy') => get<LegalDoc>(`/v1/legal/${doc}`, false),
  legalStatus: () => get<{ version: string; accepted: boolean; acceptedVersion?: string }>('/v1/legal/status'),

  // ---- discover ----
  discover: () => get<DiscoverResponse>('/v1/discover'),
  discoverSearch: (query: string) => get<{ items: DiscoverItem[] }>('/v1/discover/search?q=' + encodeURIComponent(query)),
  discoverDetail: (type: string, tmdbId: number | string) => get<DiscoverDetail>(`/v1/discover/${type}/${tmdbId}`),
  /** Cast, studio, dates, keywords and links for a Discover title. */
  discoverMoreDetails: (type: string, tmdbId: number | string) => get<ItemDetails>(`/v1/discover/${type}/${tmdbId}/details`),
  discoverEpisodes: (tmdbId: number | string, season: number) => get<{ episodes: DiscoverEpisode[] }>(`/v1/discover/tv/${tmdbId}/season/${season}`),
  /** "Your requests": the signed-in user's requests as Discover titles, with download status. */
  discoverRequests: () => get<{ items: DiscoverItem[] }>('/v1/discover/requests'),
  /** Request a title; `seasons` narrows a series (empty = everything the library lacks), `episodes` are specials ("S0E5"). */
  discoverRequest: (type: string, tmdbId: number, seasons?: number[], episodes?: string[]) =>
    post<RequestRow>('/v1/discover/request', { type, tmdbId, seasons: seasons ?? [], episodes: episodes ?? [] }),
  deleteRequest: (id: string) => del<void>(`/v1/discover/requests/${id}`),
  /**
   * Ask Sonarr / Radarr to look again for what the request still lacks: everything, or just
   * these "S1E3" episodes and / or seasons. 409 with a message when nothing can be searched.
   */
  searchAgain: (id: string, which: string[] | { episodes?: string[]; seasons?: number[] } = []) => {
    const w = Array.isArray(which) ? { episodes: which } : which
    return post<void>(`/v1/discover/requests/${id}/search`, { episodes: w.episodes ?? [], seasons: w.seasons ?? [] })
  },
  /** Admin: blocklist the failed / stalled download and let the arr app pick another release. */
  tryAnotherRelease: (id: string, episodes: string[] = []) => post<void>(`/v1/discover/requests/${id}/replace`, { episodes }),

  // ---- images ----
  /** Absolute-enough image URL (same origin) for an item's art, or null when the server has none. */
  imageUrl(item: Item, type: string, widthPx?: number): string | null {
    const rel = item.images?.[type]
    if (!rel) return null
    if (!widthPx || widthPx <= 0) return rel
    return rel + (rel.includes('?') ? '&w=' : '?w=') + Math.round(widthPx)
  },
}

/** Pixel width for the server's `?w=` resizing: CSS size times the device pixel ratio. */
export function px(cssWidth: number): number {
  return Math.round(cssWidth * Math.min(window.devicePixelRatio || 1, 3))
}
