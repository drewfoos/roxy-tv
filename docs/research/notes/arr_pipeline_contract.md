# *arr Stack <-> Custom Media Server Integration Contract (as of 2026-10-13)

Scope: request app (request UI) -> Prowlarr -> Radarr / Sonarr (anime series type) -> download client -> library import -> a NEW custom media server (replacing Jellyfin/Plex/Emby) -> user's own client. Versions observed at research time: request app v3.5.0 (released 2026-09-28), Sonarr v4.0.20.3014 (2026-09-16), Radarr v6.4.4.10685 (2026-09-16). All source citations are to the `develop` branches of the respective GitHub repositories unless stated otherwise.

---

## KQ1. What does request app (v3.x) strictly require from a "media server"?

### Takeaway
request app has no abstract "media server" interface: it supports exactly Plex, Jellyfin, or Emby, and for Jellyfin/Emby it talks to a small, well-defined subset of the Jellyfin REST API (auth, users, libraries, items, seasons/episodes). A custom server that emulates that subset (with Jellyfin-shaped JSON, `MediaBrowser` auth header, `ProviderIds`, and `MediaSources[].MediaStreams[]`) can be configured in request app as "Jellyfin" unchanged. Availability is determined only by scanning the media server; Sonarr/Radarr sync marks items PROCESSING (not AVAILABLE), and there is no inbound Sonarr/Radarr webhook in request app.

### Cited Findings

**Versions / release state**
- request app latest release is `v3.5.0`, published 2026-09-28; the release notes flag a breaking change to the Plex and Jellyfin library API endpoints (PR #3321): reading library endpoints no longer accept `sync` or `enable` params; new per-server sync and enable/disable endpoints exist — [GitHub releases API]((link removed))
- request app's Radarr/Sonarr docs: "Only v3 & V4 Radarr/Sonarr servers are supported!" — [docs.request app.dev services]((link removed))

**Media server type gating**
- The Jellyfin scanner runs only if `mediaServerType` is `JELLYFIN` or `EMBY`; it calls `animeList.sync()` before scanning — [request app server/lib/scanners/jellyfin/index.ts]((link removed))
- Jellyfin jobs (`jellyfin-recently-added-scan`, `jellyfin-full-scan`) are registered for Jellyfin or Emby; Plex jobs only for Plex — [request app server/job/schedule.ts]((link removed))
- `/auth/jellyfin` returns "Jellyfin login is disabled" if a media server is configured and either `mediaServerLogin === false` or the type is neither Jellyfin nor Emby — [request app server/routes/auth.ts]((link removed))
- `JellyfinSettings` interface: `name, ip, port, useSsl?, urlBase?, externalHostname?, jellyfinForgotPasswordUrl?, libraries: Library[], serverId, apiKey` — [request app server/lib/settings/index.ts]((link removed))

**Exact Jellyfin endpoints request app calls (class `JellyfinAPI`, server/api/jellyfin.ts)** — [request app server/api/jellyfin.ts]((link removed))
| Method | Path | Params / body | Used by |
|---|---|---|---|
| POST | `/Users/AuthenticateByName` | body `{ Username, Pw }`; optional `X-Forwarded-For` header on first attempt | `login()` |
| POST | `/QuickConnect/Initiate` | none | `initiateQuickConnect()` |
| GET | `/QuickConnect/Connect` | `?secret=` | `checkQuickConnect()` |
| POST | `/Users/AuthenticateWithQuickConnect` | body `{ Secret }` | `authenticateQuickConnect()` |
| GET | `/System/Info` | none | `getSystemInfo()` |
| GET | `/System/Info/Public` | none | `getServerName()` |
| GET | `/Users` | none | `getUsers()` |
| GET | `/Users/{userId ?? 'Me'}` | none | `getUser()` |
| GET | `/Library/MediaFolders` (primary) / `/Users/{userId ?? 'Me'}/Views` (fallback) | none | `getLibraries()` |
| GET | `/Items` | `SortBy=SortName&SortOrder=Ascending&IncludeItemTypes=Series,Movie,Others&Recursive=true&StartIndex=0&ParentId={libraryId}&collapseBoxSetItems=false` | `getLibraryContents()` |
| GET | `/Items/Latest` (Jellyfin, with `userId=`) or `/Users/{userId}/Items/Latest` (Emby) | `Limit=12&ParentId={libraryId}` | `getRecentlyAdded()` |
| GET | `/Items` | `ids={id}&fields=ProviderIds,MediaSources,Width,Height,IsHD,DateCreated` | `getItemData()` |
| GET | `/Shows/{seriesId}/Seasons` | none | `getSeasons()` |
| GET | `/Shows/{seriesId}/Episodes` | `seasonId={seasonId}` (+ `fields=MediaSources` when `includeMediaInfo`) | `getEpisodes()` |
| POST / GET | `/Auth/Keys?App={appName}` then `/Auth/Keys` | none | `createApiToken()` (creates key named "request app", then lists keys to find it) |
- Note: `/Library/VirtualFolders` and `/Items/{id}` are NOT used; item lookup is `/Items?ids=...` — [request app server/api/jellyfin.ts]((link removed))

**Auth header format request app sends** — [request app server/api/jellyfin.ts]((link removed))
```ts
let authHeaderVal = `MediaBrowser Client="request app", Device="request app", DeviceId="${safeDeviceId}", Version="${version}"`;
if (authToken) { authHeaderVal += `, Token="${authToken}"`; }
```
- Sent as the `Authorization` header (not `X-Emby-Authorization`); `Version` is `'1.0.0'` when server type is Emby, else request app's app version; default `DeviceId` is `Buffer.from('BOT_request app').toString('base64')`; `Content-Type` and `Accept` are `application/json` — [request app server/api/jellyfin.ts]((link removed))
- Per-user device IDs: admin (user id 1) always uses `BOT_request app`; other users get `Buffer.from(\`BOT_request app_${body.username}\`).toString('base64')` — [request app server/routes/auth.ts]((link removed))

**Response shapes request app types (what the custom server must return)** — [request app server/api/jellyfin.ts]((link removed))
```ts
export interface JellyfinUserResponse {
  Name: string; ServerId: string; ServerName: string; Id: string;
  Configuration: { GroupedFolders: string[] };
  Policy: { IsAdministrator: boolean };
  PrimaryImageTag?: string;
}
export interface JellyfinLibraryItem {
  Name: string; Id: string; HasSubtitles: boolean;
  Type: 'Movie' | 'Episode' | 'Season' | 'Series';
  LocationType: 'FileSystem' | 'Offline' | 'Remote' | 'Virtual';
  SeriesName?: string; SeriesId?: string; SeasonId?: string; SeasonName?: string;
  IndexNumber?: number; IndexNumberEnd?: number; ParentIndexNumber?: number;
  MediaType: string;
}
export interface JellyfinLibraryItemExtended extends JellyfinLibraryItem {
  ProviderIds: { Tmdb?: string; TheMovieDb?: string; Imdb?: string; Tvdb?: string; AniDB?: string };
  MediaSources?: JellyfinMediaSource[]; Width?: number; Height?: number; IsHD?: boolean; DateCreated?: string;
}
export interface JellyfinMediaSource { Protocol: string; Id: string; Path: string; Type: string; VideoType: string; MediaStreams: JellyfinMediaStream[]; }
export interface JellyfinMediaStream { Codec: string; Type: 'Video' | 'Audio' | 'Subtitle'; Height?: number; Width?: number; AverageFrameRate?: number; RealFrameRate?: number; Language?: string; DisplayTitle: string; }
```

**Fields the Jellyfin scanner actually reads** — [request app server/lib/scanners/jellyfin/index.ts]((link removed))
- `ProviderIds`: `Tmdb`, `TheMovieDb` (Emby spelling), `Imdb`, `Tvdb`, `AniDB`. Movie lookup order: TMDb, then IMDb (resolved to TMDb via `resolveImdbIdForScan`), then AniDB via the anime list. Shows: TMDb, then TVDb, then AniDB.
- `MediaSources[].MediaStreams[]` filtered by `Type === 'Video'`, then `Width` checked. `Height` is NOT read.
- 4K test: `return (MediaStream.Width ?? 0) > 2000;`. Movies compute `has4k` and `hasOtherResolution` separately; a standard entry is processed if `hasOtherResolution || (!this.enable4kMovie && has4k)`; a 4K entry if `has4k && this.enable4kMovie`. Episodes: 4K checked only when `enable4kShow` is on; an episode can count toward both standard and 4K totals. A code comment notes resolution alone is used, not which Radarr/Sonarr instance produced it.
- `Type` (`'Movie' | 'Series' | 'Season' | 'Episode'`) drives `processItem`; AniDB series detection uses `metadata.Type === 'Series'`.
- Item id resolution for recently-added dedupe: `const Id = jellyfinitem.SeriesId ?? jellyfinitem.SeasonId ?? jellyfinitem.Id;`; recent-mode items are deduplicated with `uniqWith` by `SeriesId`, then `SeasonId`, then `Id`.
- `IndexNumber` is used as the season number on Season items and the episode number on Episode items; `IndexNumberEnd` is used for multi-episode files (count = `IndexNumberEnd - IndexNumber + 1`); `ParentIndexNumber` is not used in this file.
- `DateCreated` becomes `mediaAddedAt`; `Name` is used as the title.
- Season matching: `Number(md.IndexNumber) === season.season_number` (TMDb season numbers); season 0 (specials) is dropped unless `settings.main.enableSpecialEpisodes` is true; a season with no matching Jellyfin season is recorded with `episodes: 0, episodes4k: 0`.
- For AniDB-tagged series: `tvdbSeasonFromAnidb === season.season_number && md.IndexNumber === 1`, counts accumulated per TMDb show id in `processedAnidbSeason`.
- `getEpisodes(seriesId, seasonId, { includeMediaInfo: true })` is called only when 4K show detection is enabled.
- The scanner is constructed as `new JellyfinAPI(getHostname(), settings.jellyfin.apiKey, admin.jellyfinDeviceId)` using the admin (user id 1) context.

**How availability status is computed (BaseScanner)** — [request app server/lib/scanners/baseScanner.ts]((link removed))
- Movies: `const statusField = is4k ? 'status4k' : 'status';`. Existing records change only when not already AVAILABLE; a title with a file and no active processing becomes AVAILABLE; an abandoned PROCESSING entry becomes UNKNOWN; otherwise PROCESSING (or stays DELETED). New movies: `if (!processing && !hasFile) { return; }`.
- Seasons: AVAILABLE when `(season.totalEpisodes === season.episodes && season.episodes > 0)`; PARTIALLY_AVAILABLE when `season.episodes > 0`; PROCESSING when `season.processing` and not DELETED; an existing AVAILABLE season is never downgraded by the scanner. `totalEpisodes` comes from TMDb `episode_count`.
- Show rollup: specials excluded; all counted seasons AVAILABLE -> `media.status = AVAILABLE`; any season PARTIALLY_AVAILABLE/AVAILABLE -> PARTIALLY_AVAILABLE; else PROCESSING/DELETED/UNKNOWN.
- `jellyfinMediaId` / `jellyfinMediaId4k` (and Plex `ratingKey`) stored on the Media row; for shows only when `season.episodes > 0`. `mediaAddedAt` set on new records or when status changes; for shows when newly available seasons increase (also updates `media.lastSeasonChange`).
- The scanner does NOT set MediaRequest status to COMPLETED; the only request status change in the base scanner is `declineOrphanedRequests` setting `request.status = MediaRequestStatus.DECLINED` when a PROCESSING entry is reset to UNKNOWN.
- Batching: `const BUNDLE_SIZE = 20;` and `const UPDATE_RATE = 4 * 1000;` (4 s between bundles); items in a bundle run via `Promise.all`; writes per TMDb id serialized via `asyncLock.dispatch(tmdbId, ...)`.

**Scan frequency (default cron, 6-field with seconds)** — [request app server/lib/settings/index.ts]((link removed))
| Job | Default schedule |
|---|---|
| `jellyfin-recently-added-scan` | `0 */5 * * * *` (every 5 min; uses `/Items/Latest?Limit=12` per library) |
| `jellyfin-full-scan` | `0 0 3 * * *` (daily 03:00; uses `/Items?Recursive=true...`) |
| `plex-recently-added-scan` / `plex-full-scan` | `0 */5 * * * *` / `0 0 3 * * *` |
| `radarr-scan` / `sonarr-scan` | `0 0 4 * * *` / `0 30 4 * * *` |
| `availability-sync` | `0 0 5 * * *` |
| `download-sync` / `download-sync-reset` | `0 * * * * *` (every minute) / `0 0 1 * * *` |
| `image-cache-cleanup` | `0 0 5 * * *` |
| `process-blocklisted-tags` | `0 30 1 */7 * *` |
- Full sync can also be triggered via `POST /settings/jellyfin/sync` (start/cancel) and progress read via GET; library list via `GET /settings/jellyfin/library`, `PUT /settings/jellyfin/library/{libraryId}` enable/disable, `POST /settings/jellyfin/library/sync`; users via `GET /settings/jellyfin/users` — [request app-api.yml]((link removed))

**Sonarr/Radarr "sync" is not availability; there is no inbound Sonarr/Radarr webhook**
- Radarr scanner: `const processing = !radarrMovie.hasFile && radarrMovie.monitored;` then `processMovie(radarrMovie.tmdbId, { is4k: server4k, serviceId, externalServiceId: radarrMovie.id, externalServiceSlug: radarrMovie.titleSlug, title, processing, hasFile: radarrMovie.hasFile })`; the file never assigns `MediaStatus.AVAILABLE` itself — [request app server/lib/scanners/radarr/index.ts]((link removed))
- request app docs describe the Radarr/Sonarr "Library scan" option as: "Enable this setting if you would like to scan your Radarr/Sonarr server for existing media/request status" (prevents duplicate requests) — [docs.request app.dev services]((link removed))
- `server/routes/index.ts` mounts `/user, /settings, /search, /discover, /request, /watchlist, /blocklist, /movie, /tv, /media, /person, /collection, /service, /issue, /issueComment, /overrideRule, /auth` and inline status/genre routes; "No `/webhook`, `/jellyfin/webhook`, `/plex/webhook`, or Sonarr/Radarr webhook path is defined or mounted in this file" — [request app server/routes/index.ts]((link removed))
- OpenAPI spec (first 100k chars): "No visible path accepts inbound webhooks from Sonarr or Radarr. Those services are configured as outbound connections (hostname, port, API key) that request app calls." The `WebhookSettings.webhookUrl` schema is for request app's own outbound notifications — [request app-api.yml]((link removed))
- Availability Sync job (`availability-sync`): for media marked AVAILABLE/PARTIALLY_AVAILABLE it checks Radarr (`if (radarr && radarr.hasFile)`), Sonarr (`if (sonarr.statistics.episodeFileCount > 0)`), Plex (`getMetadata(ratingKey)`), Jellyfin/Emby (`jellyfinClient?.getItemData(jellyfinMediaId)`; seasons: `seasonExistsInJellyfin = episodes.length > 0`). If found in none, `media[is4k ? 'status4k' : 'status'] = MediaStatus.DELETED;` and seasons via `mediaSeason[is4k ? 'status4k' : 'status'] = MediaStatus.DELETED;`. Non-404 errors are treated as "exists" to avoid false removals. "Nothing in the code removes media requests." — [request app server/lib/availabilitySync.ts]((link removed))
- request app sends requests to Sonarr with an anime profile when the TMDb show has keyword `ANIME_KEYWORD_ID = 210024` ("export const ANIME_KEYWORD_ID = 210024;"); SonarrSettings carry `seriesType`, `animeSeriesType`, `activeAnimeProfileId?`, `activeAnimeDirectory?`, `activeAnimeLanguageProfileId?`, `animeTags?`, `enableSeasonFolders`, `monitorNewItems` — [request app constants.ts]((link removed)); [request app settings/index.ts]((link removed))

**Jellyfin auth flow in request app (`POST /auth/jellyfin`)** — [request app server/routes/auth.ts]((link removed))
- Body: `username` (required), `password`, `hostname` (required only on first setup when `settings.jellyfin.ip` is empty), `port` (default 8096), `urlBase`, `useSsl`, `email`, `serverType` (must be Jellyfin or Emby enum on initial setup).
- Calls `jellyfinserver.login(username, password, clientIp)` (-> `/Users/AuthenticateByName`), then matches the local user by `account.User.Id`.
- Initial setup: `IsAdministrator === false` throws `ApiError(403, ApiErrorCode.NotAdmin)`; creates local user id 1 with `Permission.ADMIN`, storing Jellyfin user Id, device ID and access token; then `const apiKey = await jellyfinClient.createApiToken('request app');` (uses the admin's access token to POST `/Auth/Keys?App=request app` and GET `/Auth/Keys`); saves `settings.jellyfin.name` from `getServerName()` (`/System/Info/Public`), `serverId` from `account.User.ServerId`, plus `ip, port, urlBase, useSsl, apiKey`; then `startJobs()`.
- Subsequent logins: existing linked user (`account.User.Id === user.jellyfinUserId`) refreshes avatar and `jellyfinUsername`; unknown user is rejected with 403 unless `settings.main.newPlexLogin` is enabled, in which case a user is created with `defaultPermissions`.
- Avatar: `/avatarproxy/${user.jellyfinUserId}?v=${user.avatarVersion}` (served by request app's avatar proxy; the route itself makes no direct `/Users/{id}/Images/Primary` call in this file).
- Quick Connect (Jellyfin type only): `POST /jellyfin/quickconnect/initiate` -> `initiateQuickConnect()` returns `code` and `secret`; `GET /jellyfin/quickconnect/check` validates `secret` (8-128 hex chars) and calls `checkQuickConnect(secret)` returning `authenticated`; `POST /jellyfin/quickconnect/authenticate` -> `authenticateQuickConnect(secret)` (`/Users/AuthenticateWithQuickConnect`), updates `jellyfinAuthToken`, `jellyfinDeviceId`, avatar; rejected during initial setup.

**User import (`POST /user/import-from-jellyfin`, needs `MANAGE_USERS`)** — [request app server/routes/user/index.ts]((link removed))
- Calls `const jellyfinUsers = await jellyfinClient.getUsers();` (GET `/Users`) with the owner (user id 1) context; indexes by `normalizeJellyfinGuid(user.Id)`.
- Reads only `Id` and `Name` from each user; does not use `Policy.IsAdministrator` or `PrimaryImageTag` in this route.
- New users: `jellyfinUsername: jellyfinUser?.Name`, `jellyfinUserId: jellyfinUser?.Id`, `email: jellyfinUser?.Name` (placeholder), `jellyfinDeviceId` = base64 of `BOT_request app_${Name}`, `permissions: settings.main.defaultPermissions`, `avatar: /avatarproxy/${Id}`, `userType` chosen by `settings.main.mediaServerType === MediaServerType.JELLYFIN` (Jellyfin vs Emby). Body `jellyfinUserIds[]` selects which to import; existing `jellyfinUserId`s are skipped.

### Inferences
- A custom server can present itself to request app as "Jellyfin" by implementing the 15 endpoints in the table above with the typed JSON shapes; everything else in the Jellyfin API is unused by request app. The key non-obvious requirements: (a) the `Authorization: MediaBrowser Client=..., Token=...` header must be accepted for both user access tokens and API keys created via `/Auth/Keys`; (b) `/Users/AuthenticateByName` must return `{ User: { Id, Name, ServerId, Policy: { IsAdministrator }, PrimaryImageTag? }, AccessToken }` (the route reads `account.User.Id`, `account.User.ServerId`, `IsAdministrator`); (c) items must carry `ProviderIds.Tmdb` (movies, shows) or `Tvdb` (shows) or `Imdb`/`AniDB`, and `MediaSources[].MediaStreams[]` with `Type:'Video'` and `Width` so 4K detection works; (d) `/Shows/{id}/Seasons` must return Season items with `IndexNumber` = TMDb season number, and `/Shows/{id}/Episodes?seasonId=` must return Episode items with `IndexNumber` (and `IndexNumberEnd` for multi-episode files); (e) `/Items/Latest?ParentId=&Limit=12` must return the most recently added Movie/Series/Season/Episode items (with `SeriesId`/`SeasonId` for episodes) because the 5-minute recent scan relies on it; (f) `DateCreated` should be ISO-8601 so `mediaAddedAt` is correct.
- Because the scanner counts episodes against TMDb `episode_count` per TMDb season number, anime that Sonarr stores with TVDb season numbering must be exposed by the custom server with season/episode numbers that match TMDb's numbering (or with `ProviderIds.AniDB` so request app's AniDB-to-TVDb-season mapping path is used); mismatches lead to perpetual PARTIALLY_AVAILABLE.
- Availability cannot be driven by Sonarr/Radarr webhooks alone: request app has no inbound Sonarr/Radarr webhook route, the Radarr/Sonarr scanners only set PROCESSING/UNKNOWN, and the only path to AVAILABLE is the media-server scan (recent scan every 5 min by default, full scan daily). Fastest route to "available" after import is therefore for the custom server to index the new file quickly and surface it in `/Items/Latest`.
- Emby mode differs in only two places that matter for an emulator: `ProviderIds.TheMovieDb` vs `Tmdb`, and `/Users/{userId}/Items/Latest` vs `/Items/Latest?userId=`; presenting as Jellyfin is simpler.

### Gaps
- The exact JSON of `/Users/AuthenticateByName` response fields consumed (`AccessToken`, `User`) is inferred from `account.User.Id` / `account.User.ServerId` usage in auth.ts; the `login()` implementation body in jellyfin.ts was summarized, not quoted.
- Whether request app validates `/System/Info` fields (e.g., `Version`, `Id`) beyond calling it was not confirmed.
- The request app docs page for Jellyfin settings (`docs.request app.dev/using-request app/settings/jellyfin/`) returned 404 at fetch time; behaviour is documented here from source instead.
- `MediaServerType` enum numeric values live in `server/constants/server.ts` and were not fetched.

---

## KQ2. Sonarr v4 / Radarr v5-v6: import flow, naming, and Connect notifications

### Takeaway
Sonarr/Radarr import completed download client downloads (matched by category/download id) by hardlinking (default) or copying into the series/movie folder, renaming per the configured format; TRaSH's recommended formats embed `{tvdb-...}`/`{tmdb-...}`/`[tvdbid-...]`/`[tmdbid-...]` ID tags in folder names and absolute numbers, bit depth, audio language and release group for anime. The Webhook connect posts JSON with an `eventType` string and full `series`/`episodes[]`/`episodeFile` or `movie`/`movieFile` objects including TVDb/TMDb/IMDb (and for Sonarr, MAL/AniList) IDs and absolute file paths, so a custom server never has to re-parse filenames.

### Cited Findings

**Import mechanics / Completed Download Handling / hardlinks**
- "Use Hard links instead of Copy: Use Hard links when trying to copy files from torrents that are still being seeded." — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Hard links are on by default; "A hard link will not use any additional disk space."; "The file system and mounts must be the same for your completed download directory and your media library."; if linking fails Sonarr falls back to copying — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)
- Completed files stay in their original location for seeding; Sonarr hardlinks or copies them to the media folder on import. With "Remove Completed" enabled, Sonarr removes the original file and torrent only after the client reports seeding complete and the torrent is stopped — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)
- Completed Download Handling "Enable" is an advanced global setting that automatically imports completed downloads from the client; "Remove Completed" removes imported downloads from client history once finished (usenet) or stopped/complete (torrents) — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Torrent client settings: "Category" (strongly recommended), "Post-Import Category" (warned: breaks completed download removal), "Initial State" (for download client, "Forced" bypasses all seed thresholds) — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Remote Path Mappings: "A remote path map is a DUMB search/replace" mapping the path the download client reports to a path Sonarr can access; used mainly when Sonarr and the client are on different hosts or with merged filesystems — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Other Media Management options: "Import Extra Files" (examples "subtitles, nfo, etc"; subtitle tags such as `cc` or `forced` are preserved when recognized); "Rescan Series Folder after Refresh" (Always / After Manual Refresh / Never, with Never "not recommended unless all library changes are made through Sonarr"); "Change File Date" (None / Local Release / UTC Release Date); "Minimum Free Space" — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Series Type definition: "**Anime** - Episodes released using an absolute episode number"; "the Anime type does not have any concept of Season Packs or Seasons" — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)

**Naming conventions (TRaSH Guides, Sonarr)** — [TRaSH Sonarr naming](https://trash-guides.info/Sonarr/Sonarr-recommended-naming-scheme/)
- Standard Episode Format begins `{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {Episode CleanTitle:90}` followed by custom formats, quality, audio codec/channels, dynamic range, video codec and `-{Release Group}`. Example: `The Series Title! (2010) - S01E01 - Episode Title 1 [AMZN WEBDL-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp`; multi: `... - S01E01-E03 - Episode Title [...]-RlsGrp`.
- Anime Episode Format begins `{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {absolute:000} - {Episode CleanTitle:90}` and adds bit depth next to the video codec and an audio-language token. Example: `The Series Title! (2010) - S01E01 - 001 - Episode Title 1 [iNTERNAL HDTV-720p v2][HDR10][10bit][x264][DTS 5.1][JA]-RlsGrp`; multi-episode uses an absolute range `001-003`.
- Daily: `{Series CleanTitleWithoutYear} {(Series Year)} - {Air-Date} - {Episode CleanTitle:90}` e.g. `... - 2013-10-30 - Episode Title 1 [...]`.
- Series Folder Format: standard `{Series CleanTitleWithoutYear} {(Series Year)}`; Plex adds `{imdb-{ImdbId}}` or `{tvdb-{TvdbId}}`; Emby `[imdb-{ImdbId}]` / `[tvdb-{TvdbId}]`; Jellyfin TVDb-only `[tvdbid-{TvdbId}]` ("Jellyfin does not support IMDb IDs for shows"). Season Folder Format: `Season {season:00}` -> `Season 01`. TVDb IDs are preferred because they "guarantee a match".
- Alternative "P2P/Scene" style: `The.Series.Title's!.2010.S01E01.Episode.Title.1.ATVP.WEBDL-2160p.EAC3.Atmos.5.1.DV.HDR10Plus.h265-RlsGrp`.
- Sonarr naming tokens: `{absolute:0}` / `{absolute:00}` / `{absolute:000}` (e.g. 001); `{Release Group}`; `{MediaInfo VideoBitDepth}` (e.g. 8); `{MediaInfo AudioLanguages}` (e.g. `[EN+DE]`, filterable `:EN+DE`, omitted when the only audio language is English) — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)

**Naming conventions (TRaSH Guides, Radarr)** — [TRaSH Radarr naming](https://trash-guides.info/Radarr/Radarr-recommended-naming-scheme/)
- Standard Movie Format: `{Movie CleanTitle} {(Release Year)} - {{Edition Tags}}` + `{[MediaInfo 3D]}{[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}` + `{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}`. Example: `The Movie Title (2010) - {Ultimate Extended Edition} [3D][CF Name][Bluray-2160p Proper][EAC3 Atmos 5.1][DV HDR10][x265]-RlsGrp`.
- ID-tag variants: Plex `{imdb-{ImdbId}}` / `{tmdb-{TmdbId}}` after the year (edition becomes `{edition-{Edition Tags}}`); Emby `[imdb-{ImdbId}]` / `[tmdb-{TmdbId}]`; Jellyfin `[imdbid-{ImdbId}]` / `[tmdbid-{TmdbId}]`. Example: `The Movie Title (2010) [imdbid-tt0106145] - {Ultimate Extended Edition} [...]`.
- Movie Folder Format: `{Movie CleanTitle} ({Release Year})`, Plex `... {tmdb-{TmdbId}}`, Emby `... [tmdb-{TmdbId}]`, Jellyfin `... [tmdbid-{TmdbId}]`; examples `The Movie Title (2010) {imdb-tt1520211}`, `The Movie Title (2010) [tmdbid-1520211]`.
- "Please note that folder names are created in the database when the movie is added to Radarr" (so a folder ID tag can be missing/stale); putting the ID in the filename lets Radarr "pull a fresh ID for any download or upgrade"; warning that "adding anything additional after the release year could give issues during a fresh import into Radarr".

**Webhook connect: event types and payloads**
- Sonarr `WebhookEventType` enum is serialized with `[JsonConverter(typeof(StringEnumConverter), converterParameters: typeof(DefaultNamingStrategy))]`; members in order: `Test, Grab, Download, Rename, SeriesAdd, SeriesDelete, EpisodeFileDelete, Health, ApplicationUpdate, HealthRestored, ManualInteractionRequired` — [Sonarr WebhookEventType.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookEventType.cs)
- Sonarr payload builders (WebhookBase.cs) — [Sonarr WebhookBase.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookBase.cs):
  - Grab: `EventType = Grab`, `Series`, `Episodes`, `Release` (WebhookRelease), `CustomFormatInfo`, `DownloadClient`, `DownloadClientType`, `DownloadId`, `InstanceName`, `ApplicationUrl`.
  - Download (import/upgrade): `EventType = Download`, `Series`, `Episodes`, `EpisodeFile` (with `SourcePath`), `Release` (WebhookGrabbedRelease), `CustomFormatInfo`, `IsUpgrade = message.OldFiles.Any()`, `DeletedFiles` (list of WebhookEpisodeFile with `Path` and `RecycleBinPath`, only when old files exist), `DownloadClient`, `DownloadClientType`, `DownloadId`.
  - Import Complete: also `EventType = Download` (same string), with `EpisodeFiles` list, `SourcePath`, `DestinationPath`, no CustomFormatInfo.
  - EpisodeFileDelete: `Series`, `Episodes`, `EpisodeFile`, `DeleteReason`. SeriesAdd: `Series`. SeriesDelete: `Series`, `DeletedFiles`. Rename: `Series`, `RenamedEpisodeFiles`. Health/HealthRestored: `Level, Message, Type, WikiUrl`. ApplicationUpdate: `Message, PreviousVersion, NewVersion`. ManualInteractionRequired: `Series`, `Episodes`, `DownloadInfo`, `DownloadStatus`, `DownloadStatusMessages`, `CustomFormatInfo`, `Release`. Test: literal `Series` and one `Episode` with `Id, EpisodeNumber, SeasonNumber, Title`.
- Sonarr `WebhookSeries` fields: `Id, Title, TitleSlug, Path, TvdbId, TvMazeId, TmdbId, ImdbId, MalIds (HashSet<int>), AniListIds (HashSet<int>), Type (SeriesTypes), Year, Genres, Images, Tags, OriginalLanguage, OriginalCountry` — [Sonarr WebhookSeries.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookSeries.cs)
- Sonarr `WebhookEpisode` fields: `Id, EpisodeNumber, SeasonNumber, Title, Overview, AirDate (string), AirDateUtc (DateTime?), SeriesId, TvdbId, FinaleType` — [Sonarr WebhookEpisode.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookEpisode.cs)
- Sonarr `WebhookEpisodeFile` fields: `Id, RelativePath, Path = Path.Combine(series.Path, RelativePath), Quality (name), QualityVersion, ReleaseGroup, SceneName, Size, DateAdded, Languages, MediaInfo (when present), SourcePath, RecycleBinPath` — [Sonarr WebhookEpisodeFile.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookEpisodeFile.cs)
- Sonarr `WebhookEpisodeFileMediaInfo`: `AudioChannels (decimal), AudioCodec, AudioLanguages (List<string>, distinct), Height, Width, Subtitles (List<string>), VideoCodec, VideoDynamicRange, VideoDynamicRangeType` — [Sonarr WebhookEpisodeFileMediaInfo.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookEpisodeFileMediaInfo.cs)
- Radarr payload builders — [Radarr WebhookBase.cs](https://raw.githubusercontent.com/Radarr/Radarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookBase.cs): Grab (`Movie, RemoteMovie, Release, DownloadClient, DownloadClientType, DownloadId, CustomFormatInfo`); Download (`Movie, RemoteMovie, MovieFile` with `SourcePath`, `Release`, `IsUpgrade`, `DeletedFiles`, `DownloadClient*`, `DownloadId`, `CustomFormatInfo`); Rename (`Movie, RenamedMovieFiles`); MovieAdded (`Movie, AddMethod`); MovieDelete (`Movie, DeletedFiles, MovieFolderSize`); MovieFileDelete (`Movie, MovieFile, DeleteReason`); Health/HealthRestored; ApplicationUpdate; ManualInteractionRequired; Test. EventType members used: `Grab, Download, Rename, MovieAdded, MovieDelete, MovieFileDelete, Health, HealthRestored, ApplicationUpdate, ManualInteractionRequired, Test`.
- Radarr `WebhookMovie` fields: `Id, Title, Year, FilePath = Path.Combine(movie.Path, movieFile.RelativePath)` (3-arg ctor only), `ReleaseDate` (physical release, `yyyy-MM-dd`), `FolderPath = movie.Path`, `TmdbId, ImdbId, Overview, Genres, Images, Tags, OriginalLanguage` — [Radarr WebhookMovie.cs](https://raw.githubusercontent.com/Radarr/Radarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookMovie.cs)
- Radarr `WebhookMovieFile` fields: `Id, RelativePath, Path = Path.Combine(movie.Path, RelativePath), Quality, QualityVersion, ReleaseGroup, SceneName, IndexerFlags (string), Size, DateAdded, Languages, MediaInfo, SourcePath, RecycleBinPath` — [Radarr WebhookMovieFile.cs](https://raw.githubusercontent.com/Radarr/Radarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookMovieFile.cs)
- Sonarr Connect triggers list: On Grab, On Import, On Upgrade, On Import Complete, On Rename, On Series Add, On Series Delete, On Episode File Delete, On Episode File Delete For Upgrade, On Health Issue (option to include warnings), On Health Restored, On Application Update, On Manual Interaction Required — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)

**Custom Script connect (environment variables)**
- Sonarr On Import/On Upgrade provides (all events also include `sonarr_eventtype`, `sonarr_instancename`, `sonarr_applicationurl`): `sonarr_isupgrade`, `sonarr_series_id`, `sonarr_series_title`, `sonarr_series_titleslug`, `sonarr_series_path`, `sonarr_series_tvdbid`, `sonarr_series_tvmazeid`, `sonarr_series_tmdbid`, `sonarr_series_imdbid`, `sonarr_series_type`, `sonarr_series_year`, `sonarr_series_originallanguage`, `sonarr_series_genres`, `sonarr_series_tags`, `sonarr_episodefile_id`, `sonarr_episodefile_episodecount`, `sonarr_episodefile_relativepath`, `sonarr_episodefile_path`, `sonarr_episodefile_episodeids`, `sonarr_episodefile_seasonnumber`, `sonarr_episodefile_episodenumbers`, `sonarr_episodefile_episodeairdates`, `sonarr_episodefile_episodeairdatesutc`, `sonarr_episodefile_episodetitles`, `sonarr_episodefile_episodeoverviews`, `sonarr_episodefile_quality`, `sonarr_episodefile_qualityversion`, `sonarr_episodefile_releasegroup`, `sonarr_episodefile_scenename`, `sonarr_episodefile_sourcepath`, `sonarr_episodefile_sourcefolder`, `sonarr_episodefile_mediainfo_{audiochannels,audiocodec,audiolanguages,languages,height,width,subtitles,videocodec,videodynamicrangetype}`, `sonarr_episodefile_customformat`, `sonarr_episodefile_customformatscore`, `sonarr_download_client`, `sonarr_download_client_type`, `sonarr_download_id`, `sonarr_release_{indexer,size,title,releasetype}`, `sonarr_deletedrelativepaths`, `sonarr_deletedpaths`, `sonarr_deleteddateadded`, `sonarr_deletedrecyclebinpaths`. On Grab adds `sonarr_release_absoluteepisodenumbers` among `sonarr_release_*`. On Rename: `sonarr_episodefile_ids`, `_relativepaths`, `_paths`, `_previousrelativepaths`, `_previouspaths`. On Episode File Delete: `sonarr_episodefile_deletereason` + file fields. On Series Delete: `sonarr_series_deletedfiles`. Test: `sonarr_eventtype=Test` — [Servarr Wiki sonarr/custom-scripts.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/custom-scripts.md)
- Radarr On Import/On Upgrade: `radarr_eventtype, radarr_instancename, radarr_applicationurl, radarr_isupgrade, radarr_movie_id, radarr_movie_title, radarr_movie_year, radarr_movie_originallanguage, radarr_movie_genres, radarr_movie_tags, radarr_movie_path, radarr_movie_imdbid, radarr_movie_tmdbid, radarr_movie_in_cinemas_date, radarr_movie_physical_release_date, radarr_movie_overview, radarr_moviefile_id, radarr_moviefile_relativepath, radarr_moviefile_path, radarr_moviefile_quality, radarr_moviefile_qualityversion, radarr_moviefile_releasegroup, radarr_moviefile_scenename, radarr_moviefile_sourcepath, radarr_moviefile_sourcefolder, radarr_moviefile_customformat, radarr_moviefile_customformatscore, radarr_release_indexer, radarr_release_size, radarr_release_title, radarr_download_client, radarr_download_client_type, radarr_download_id, radarr_deletedrelativepaths, radarr_deletedpaths, radarr_deleteddateadded, radarr_deletedrecyclebinpaths`, plus `radarr_moviefile_mediainfo_{audiochannels,audiocodec,audiolanguages,languages,height,width,subtitles,videocodec,videodynamicrangetype}`. On Rename: `radarr_moviefile_ids/_relativepaths/_paths/_previousrelativepaths/_previouspaths`. On Movie Delete: `radarr_movie_deletedfiles`, `radarr_movie_folder_size`. On Movie File Delete: `radarr_moviefile_deletereason`, file fields incl. `radarr_moviefile_size`. On Movie Added: `radarr_movie_addmethod` — [Servarr Wiki radarr/custom-scripts.md](https://raw.githubusercontent.com/Servarr/Wiki/master/radarr/custom-scripts.md)

**Emby/Jellyfin connect: what the receiving side (Jellyfin) exposes**
- Jellyfin `LibraryController`: `[HttpPost("Library/Media/Updated")] [Authorize] public ActionResult PostUpdatedMedia([FromBody, Required] MediaUpdateInfoDto dto)`; body `foreach (var item in dto.Updates) { _libraryMonitor.ReportFileSystemChanged(item.Path ?? throw new ArgumentException("Item path can't be null.")); }` returning 204 `NoContent()`; only `Path` is used. Also `[HttpPost("Library/Refresh")]` with `[Authorize(Policy = Policies.RequiresElevation)]` -> `ValidateMediaLibrary`; `Library/Movies/Updated` (matches by IMDb then TMDb id) and `Library/Series/Updated` (matches by TVDB id) each call `ReportFileSystemChanged(item.Path)` — [jellyfin LibraryController.cs](https://raw.githubusercontent.com/jellyfin/jellyfin/master/Jellyfin.Api/Controllers/LibraryController.cs)
- `public class MediaUpdateInfoDto { public IReadOnlyList<MediaUpdateInfoPathDto> Updates { get; set; } = Array.Empty<MediaUpdateInfoPathDto>(); }` — [jellyfin MediaUpdateInfoDto.cs](https://raw.githubusercontent.com/jellyfin/jellyfin/master/Jellyfin.Api/Models/LibraryDtos/MediaUpdateInfoDto.cs)
- Sonarr FAQ notes for Plex/Emby that after a folder rename the library update "will trigger re-detection of intros, thumbnails, chapters, and preview metadata" — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)

**Sonarr metadata/XEM/absolute numbering**
- Skyhook refresh runs every 12 hours and cannot be disabled; Sonarr's Skyhook has "a much smaller few hour cache" on top of TVDb's, so changes take 3-19 hours; only English episode titles sync; Sonarr does not use TVDb aliases/translations — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)
- "Sonarr relies on TheXEM, a community driven site that lets users create mappings" between scene and TVDb numbering; Xem is "the only way to fix Japanese anime aliases"; some release groups follow TVDb rather than Scene — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)
- Episodes with absolute numbers added to TVDb (via Skyhook) are auto-searched if aired within last 14 days; "The episode(s) on TVDb do not have an absolute number assigned" is the usual cause of missed anime, fix on TVDb then wait 36-48 h — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)

### Inferences
- A custom server should consume the Sonarr/Radarr Webhook (POST JSON) and key on: `eventType` (string, e.g. `"Download"`, `"Rename"`, `"EpisodeFileDelete"`, `"SeriesDelete"`, `"MovieFileDelete"`, `"MovieDelete"`, `"Test"`, `"Health"`), `series.tvdbId` / `series.tmdbId` / `series.imdbId` / `series.malIds[]` / `series.anilistIds[]` / `series.type` (`anime`), `episodes[].seasonNumber` / `episodes[].episodeNumber` / `episodes[].tvdbId`, `episodeFile.path` (absolute, server-side path = `series.path` + `relativePath`), `episodeFile.mediaInfo.{width,height,audioLanguages,subtitles,videoCodec}`, `isUpgrade`, `deletedFiles[].path`; and for Radarr `movie.tmdbId` / `movie.imdbId` / `movie.folderPath` / `movieFile.path`. JSON property names are camelCase in practice (consistent with request app's `radarrMovie.hasFile`/`tmdbId` consumption of the same serializer), but see Gaps.
- `Rename` payloads carry previous and new paths (`renamedEpisodeFiles` / `renamedMovieFiles`; custom script `*_previouspaths` / `*_paths`), so the server should re-key its index by the new path without re-scanning.
- To be a drop-in for the built-in "Emby/Jellyfin" connect (if the user prefers it over Webhook), the custom server needs `POST /Library/Media/Updated` accepting `{ "Updates": [ { "Path": "...", "UpdateType": "..." } ] }` with a token-bearing auth header, returning 204, and optionally `POST /Library/Refresh`. Paths arrive as Sonarr/Radarr sees them (container paths) unless the connect's path mapping is used, so the server must either share the same mount layout or map paths.
- Polling as a fallback: Sonarr `/api/v3/series`, `/api/v3/episode?seriesId=`, `/api/v3/episodefile?seriesId=`; Radarr `/api/v3/movie`, `/api/v3/moviefile?movieId=` with `X-Api-Key` give the same ids and absolute paths; request app's own Radarr scanner relies on `getMovies()` (`/movie`) returning `id, tmdbId, titleSlug, hasFile, monitored`, and the availability sync relies on Sonarr series `statistics.episodeFileCount`, which confirms those fields exist on the v3 API.
- The TRaSH Jellyfin folder tags `[tvdbid-NNN]` / `[tmdbid-NNN]` / `[imdbid-ttNNN]` are a reliable secondary key the server can regex from folder names if a webhook is missed; absolute episode numbers `- 001 -` and `[10bit]`, `[JA]`, `-RlsGrp` tokens follow the anime format above.

### Gaps
- The Sonarr/Radarr `MediaBrowserProxy.cs` (Emby/Jellyfin connect implementation) could not be fetched (404 on raw/blob URLs for `src/NzbDrone.Core/Notifications/Emby/MediaBrowserProxy.cs` on both repos), so the exact `UpdateType` values sent, the auth header used (`X-MediaBrowser-Token` vs `Authorization: MediaBrowser ... Token=`), and whether Sonarr calls `/Library/Media/Updated` vs `/Library/Refresh` for Jellyfin are unverified here; only the Jellyfin receiving contract is cited.
- The JSON casing of webhook payloads (camelCase vs PascalCase) was not directly confirmed from the Sonarr/Radarr JSON serializer settings; property names above are the C# names.
- The Servarr "sonarr/api" wiki page and `sonarr.tv/docs/api` were not reachable; the `X-Api-Key` header / `apikey` query param convention is stated from general knowledge, not a fetched source.
- download client-specific download client fields (Sequential Order, First and Last First, Content Layout) are not on the fetched settings page.
- Sonarr's default (non-TRaSH) anime naming format and the "Anime Standard Format Search" option details were not captured.

---

## KQ3. Prowlarr: role and media-server requirements

### Takeaway
Prowlarr only manages indexers and pushes them into Sonarr/Radarr; it never talks to the media server, and its download clients are used only for its own in-app searches.

### Cited Findings
- "The first thing to set up in Prowlarr is indexers." Apps are connected under Settings > Apps; indexers sync based on supported categories — [Servarr Wiki prowlarr/quick-start-guide.md](https://raw.githubusercontent.com/Servarr/Wiki/master/prowlarr/quick-start-guide.md)
- Sync Level: "Add and Remove Only" updates the remote app when indexers are added/removed; "Full Sync" keeps the app's indexers matched to Prowlarr, overwriting remote edits — [Servarr Wiki prowlarr/quick-start-guide.md](https://raw.githubusercontent.com/Servarr/Wiki/master/prowlarr/quick-start-guide.md)
- "Enter the Prowlarr server URL (including http, port, and baseurl if needed) as the app would access it here."; "Download clients are for Prowlarr in-app searches only and do not sync to apps."; the guide does not mention media servers at all — [Servarr Wiki prowlarr/quick-start-guide.md](https://raw.githubusercontent.com/Servarr/Wiki/master/prowlarr/quick-start-guide.md)
- Sonarr: "if none of your enabled indexers have any anime categories configured then it effectively skips the indexer" for Anime series type searches — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)

### Inferences
- Nothing is required from the custom media server for Prowlarr. The only anime-relevant Prowlarr setting is that synced indexers must expose anime categories (TV/Anime 5070) for Sonarr's Anime series type to search them.

### Gaps
- None material.

---

## KQ4. download client: categories, seeding, hardlinks, Docker volume layout

### Takeaway
Use one `/data` volume shared by download client, Sonarr, Radarr and the media server, with downloads in `/data/torrents/{tv,movies}` (via download client categories with "Automatic" torrent management) and the library in `/data/media/{tv,movies}`; this lets Sonarr/Radarr hardlink (instant, zero extra space) while seeding continues, and the media server sees the finished file in `/data/media` immediately.

### Cited Findings
- Recommended tree rooted at `data`: `torrents/{books,movies,music,tv}`, `usenet/{incomplete,complete/{...}}`, `media/{books,movies,music,tv}`; `media` is "your library and what you'd pass to Plex, Emby or JellyFin"; creation command `mkdir -p /data/{torrents/{tv,movies,music},media/{tv,movies,music}}` — [TRaSH Docker folder structure](https://trash-guides.info/File-and-Folder-Structure/How-to-set-up/Docker/)
- download client container path `/data/torrents/` mapped from host `/<path_to_data>/data/torrents/`; sort downloads into `/data/torrents/{tv|movies|music}`; Sonarr/Radarr "gets access to everything using `/data`"; compose volumes: radarr/sonarr `/data:/data`, bazarr `/data/media:/data/media` — [TRaSH Docker folder structure](https://trash-guides.info/File-and-Folder-Structure/How-to-set-up/Docker/)
- Default mounts like `/movies` or `/downloads` make containers "look like two or three file systems, even if they aren't," losing "the ability to hardlink or instant move," forcing slower copy+delete — [TRaSH Docker folder structure](https://trash-guides.info/File-and-Folder-Structure/How-to-set-up/Docker/)
- Hardlinks: "Having a file in multiple locations without using double your storage space."; "You CAN'T hardlink across separate file systems, partitions, volumes or mounts."; "Space is only reclaimed once all "copies" of hardlinked data are deleted."; "Modifying any copy of a hardlinked file will impact all "copies"."; an instant move "is a real move, not a copying of a file from the download folder to the media folder" — [TRaSH Hardlinks and Instant Moves](https://trash-guides.info/File-and-Folder-Structure/Hardlinks-and-Instant-Moves/)
- download client setup: Saving Management "Make sure this is set to `Automatic`" (Default Torrent Management Mode); "Pre-allocate disk space for all files" is personal preference (disable on unRAID cache); seeding limits (ratio/total time/inactive time) suggested "Disabled" with action "Paused and Disabled"; warns against Docker-style `/downloads` => `/data/downloads` mounts — [TRaSH download client Basic Setup](https://trash-guides.info/Downloaders/download client/Basic-Setup/)
- Categories: enter only a subfolder in the category Save Path (it is relative to the default download root); example layout `/data/torrents/{tv|movies|music}`; if downloads land in `/data/torrents/` instead of the category folder "you probably forgot to change `Default Torrent Management Mode:` to `Automatic`" — [TRaSH download client categories](https://trash-guides.info/Downloaders/download client/How-to-add-categories/)
- Sonarr: "Category" strongly recommended; "Post-Import Category" breaks completed download removal; "Remove Completed" only after seed goals reached and torrent stopped — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)

### Inferences
- The custom media server container must mount the same host path at the same container path (`/data` or at least `/data/media`) as Sonarr/Radarr so that paths in webhook payloads (`episodeFile.path`, `movieFile.path`, `series.path`, `movie.folderPath`) resolve without translation. If paths differ, the server must implement its own "remote path mapping" (prefix replace).
- Because imports are hardlinks, a file may appear in `/data/media` while its twin is still seeding in `/data/torrents`; the server should index only the `/data/media` tree and must never modify files in place (would corrupt the seeded copy and the torrent).
- Upgrades delete the old library file (`deletedFiles[]` in the Download payload) and add a new one; the server must handle delete+add on the same episode/movie in one event.

### Gaps
- TRaSH's category page does not prescribe category names; the conventional `tv`/`movies` (or `tv-sonarr`/`radarr`) pairing with Sonarr/Radarr's "Category" field is an inference.
- The Docker structure page was marked "Docs built by Pull Request - Do not use these" at fetch time, so wording may differ from the published page.

---

## KQ5. Bazarr (optional): sidecar subtitle naming the server must parse

### Takeaway
Bazarr writes sidecar subtitles as `<video basename>.<lang>[.forced|.<hi-ext>][.<custom tags>].srt` (or `.<format>` for non-SRT), where the hearing-impaired suffix is configurable as `hi`, `cc` or `sdh`, forced and HI are mutually exclusive, and a "single language" mode drops the language suffix entirely; files may be placed alongside the video or in a configured sub-folder.

### Cited Findings
- Bazarr `general` settings and defaults: `subfolder` default `'current'`; `subfolder_custom` default `''`; `hi_extension` default `'hi'`, `is_in=['hi', 'cc', 'sdh']`; `single_language` default `False`; `use_embedded_subs` default `True`; `embedded_subs_show_desired` default `True`; `ignore_pgs_subs`/`ignore_vobsub_subs`/`ignore_ass_subs` default `False`; `utf8_encode` default `True`; `postprocessing_cmd` default `''`; `use_postprocessing` default `False`; `postprocessing_threshold` default `90`; `chmod` default `'0640'`; `language_equals` default `[]` — [bazarr app/config.py](https://raw.githubusercontent.com/morpheus65535/bazarr/master/bazarr/app/config.py)
- Filename builder `get_subtitle_path(video_path, language=None, extension='.srt', forced_tag=False, hi_tag=False, tags=None)`: appends `'.' + str(language.basename)`; adds `forced` when `forced_tag`; HI tag only in an `elif` branch (skipped when forced) using `SZ_HI_EXTENSION` (default `"hi"`); custom `tags` appended as `".%s" % "-".join(tags)`; if `language` is `None` no suffix at all (`save_subtitles` passes `None` when `single` is true); extension default `.srt`, swapped to `.<format>` for non-SRT formats; `cc`/`sdh` are recognised when scanning existing files in `_search_external_subtitles`; `save_subtitles(directory=...)` relocates the file to a given folder; `search_external_subtitles` treats `CUSTOM_PATHS` entries as absolute or relative to the video folder — [bazarr subliminal_patch/core.py](https://raw.githubusercontent.com/morpheus65535/bazarr/master/custom_libs/subliminal_patch/core.py)
- Bazarr maintainer (2019 feature thread): Bazarr names forced subs like `subs.en.forced.srt` and at that time had no HI suffix "because there is no standard" — [Bazarr feature upvote thread](https://bazarr.featureupvote.com/suggestions/90596/comment/474679); a user request noted Bazarr "simply truncates whatever proceeds the 3-character limit" when custom suffixes are attempted — [Bazarr feature upvote: customize language extensions](https://bazarr.featureupvote.com/suggestions/63289/customize-language-extensions)
- Sonarr "Import Extra Files" preserves subtitle tags such as `cc` or `forced` when recognised — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)

### Inferences
- Parser for the server: strip the video extension, then match `^(?P<base>.+?)(?:\.(?P<lang>[a-z]{2,3}(?:-[A-Za-z]{2,4})?))?(?:\.(?P<forced>forced))?(?:\.(?P<hi>hi|cc|sdh))?(?:\.(?P<tags>[^.]+))?\.(?P<ext>srt|ass|ssa|vtt|sub|idx|sup)$`. Treat `forced` and `hi|cc|sdh` as mutually exclusive, default language to "undetermined" when absent (single-language mode), and respect a configurable sub-folder (`subfolder` = current/relative/absolute). Since `.ass` sidecars are only written when the provider format is ASS (Bazarr swaps the extension for non-SRT formats) and `ignore_ass_subs` exists for embedded ASS, anime libraries will mostly carry embedded ASS in MKV plus occasional `.ass` sidecars.
- Bazarr needs read/write to `/data/media` only (compose mounts `/data/media:/data/media`), so the custom server should expect subtitles to appear asynchronously after the video, and watch for sidecar creation (filesystem watcher or Bazarr post-processing command / webhook) rather than only on Sonarr/Radarr webhooks.

### Gaps
- The Bazarr wiki settings page (`wiki.bazarr.media/Additional-Configuration/Settings/`) returned 404, so the UI labels for `subfolder` modes (`current`/`relative`/`absolute`) and the exact language-code style (alpha-2 vs alpha-3; `language.basename` comes from Bazarr's custom language object) are not confirmed from docs. The regex above is my inference.
- Whether Bazarr v1.5.x writes `pt-BR`/`zh-TW` style codes with country suffix was not verified.

---

## KQ6. Anime specifics: series type, numbering, release flags, metadata providers and ID mapping

### Takeaway
Sonarr v4 treats Anime as absolute-numbered (no season packs), maps scene numbering via TheXEM, pulls TVDb data via Skyhook (now including MAL and AniList IDs in webhook `series.malIds`/`series.aniListIds`), and TRaSH's anime format emits `SxxEyy - abs - title [..][10bit][codec][audio][JA]-Group`; Fribb/anime-lists provides a merged AniDB-keyed JSON mapping to AniList/MAL/Kitsu/TVDb/TMDb/IMDb with TVDb season and episode offsets, which request app itself consumes for `ProviderIds.AniDB`.

### Cited Findings
- Sonarr Anime series type: "Episodes released using an absolute episode number"; "the Anime type does not have any concept of Season Packs or Seasons" so each episode is searched individually; try Standard type if problems; Xem is "the only way to fix Japanese anime aliases"; dubbed vs subbed tracking example (Pokemon tracks dubbed on TheXEM) — [Servarr Wiki sonarr/faq.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/faq.md)
- Sonarr webhook `WebhookSeries` now includes `MalIds (HashSet<int>)` and `AniListIds (HashSet<int>)` alongside `TvdbId, TvMazeId, TmdbId, ImdbId, Type` — [Sonarr WebhookSeries.cs](https://raw.githubusercontent.com/Sonarr/Sonarr/develop/src/NzbDrone.Core/Notifications/Webhook/WebhookSeries.cs)
- TRaSH anime filename example: `The Series Title! (2010) - S01E01 - 001 - Episode Title 1 [iNTERNAL HDTV-720p v2][HDR10][10bit][x264][DTS 5.1][JA]-RlsGrp`; only the anime format includes `{absolute:000}` and bit depth; audio language shown as `[JA]`; release group always last after a hyphen — [TRaSH Sonarr naming](https://trash-guides.info/Sonarr/Sonarr-recommended-naming-scheme/)
- Sonarr `{MediaInfo AudioLanguages}` renders e.g. `[EN+DE]` (dual audio appears as `[JA+EN]`-style token) and omits the token when the only language is English; `{MediaInfo VideoBitDepth}` renders `8`/`10` — [Servarr Wiki sonarr/settings.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/settings.md)
- Custom-script `sonarr_release_absoluteepisodenumbers` (On Grab) and `sonarr_episodefile_mediainfo_audiolanguages` / `_subtitles` (On Import) expose absolute numbers and language lists without filename parsing — [Servarr Wiki sonarr/custom-scripts.md](https://raw.githubusercontent.com/Servarr/Wiki/master/sonarr/custom-scripts.md)
- Fribb/anime-lists files: `anime-offline-database-reduced`, `anime-lists-reduced`, `anime-list-full` (merged by `anidb_id`), `anime-list-mini` (minified), `Collections/` (per-source related-anime files, e.g. `anidb_collection.json`), `Indices/` (per-source lookup files e.g. mal, tmdb, tvdb) — [Fribb/anime-lists README](https://raw.githubusercontent.com/Fribb/anime-lists/master/README.md)
- `anime-list-full` entry fields: `type, anidb_id, anilist_id, animecountdown_id, animenewsnetwork_id, anime-planet_id, anisearch_id, kitsu_id, livechart_id, mal_id, simkl_id, imdb_id (array), themoviedb_id ({tv: id} or {movie: [ids]}), tvdb_id, season ({tvdb, tmdb}), episode_offset ({tvdb, tmdb})`; sources are cedya77/anime-offline-database (fork) and Anime-Lists/anime-lists; caveats: Anime-Lists contains TVDb IDs set to "movie", TVDb/TMDb share numeric IDs across movie/TV so the right endpoint must be used, missing IDs are filled via TMDb API without accuracy guarantees, no stated update schedule — [Fribb/anime-lists README](https://raw.githubusercontent.com/Fribb/anime-lists/master/README.md)
- request app's Jellyfin scanner reads `ProviderIds.AniDB`, calls `animeList.sync()` before scanning, resolves AniDB -> TMDb/TVDb season via the anime list, and matches `tvdbSeasonFromAnidb === season.season_number && md.IndexNumber === 1`; shows with TMDb keyword `210024` use the anime metadata provider — [request app jellyfin scanner]((link removed)); [request app constants.ts]((link removed))

### Inferences
- The custom server should treat the Sonarr webhook as the authoritative numbering source for anime: `series.tvdbId` + `episodes[].seasonNumber/episodeNumber` (TVDb numbering, already XEM-corrected by Sonarr) plus `series.malIds[]`/`series.aniListIds[]` for AniList/MAL metadata, and compute AniDB ids by reverse lookup in Fribb's `Indices/` (tvdb -> anidb with `season`/`episode_offset`). Absolute numbers can be recovered from the filename's `- NNN -` token (TRaSH format) or from `sonarr_release_absoluteepisodenumbers` at grab time; Sonarr's import webhook `episodes[]` object does not carry an absolute number field (see Gaps).
- To stay request app-compatible for anime, expose shows with `ProviderIds.Tmdb` (preferred) and `Tvdb`, and seasons numbered as TMDb numbers them; expose `ProviderIds.AniDB` only if the server deliberately organises by AniDB season (then request app will map via its anime list).
- Dual-audio / 10-bit / release-group flags are reliably available structured in the import webhook (`episodeFile.mediaInfo.audioLanguages[]`, `episodeFile.releaseGroup`, `episodeFile.quality`) and only heuristically in filenames (`[JA+EN]`, `[10bit]`, `-Group`); `mediaInfo` does not include bit depth, so bit depth must be probed by the server (ffprobe) if needed.

### Gaps
- Sonarr `WebhookEpisode` has no `absoluteEpisodeNumber` property (only `EpisodeNumber, SeasonNumber, TvdbId...`); whether a newer Sonarr build adds it was not verified beyond the fetched `develop` file.
- Shoko and AniList/AniDB API specifics (endpoints, rate limits) were not researched; Shoko's own ID-mapping was not fetched.
- Fribb's update cadence is not documented in the README.
- Sonarr's exact XEM endpoint usage (thexem.info API) and Skyhook URL were described by the wiki only at a high level.
