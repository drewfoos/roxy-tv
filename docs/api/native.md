# Roxy native API (v1)

The API Roxy's own clients (Roxy TV app, Roxy Web) speak. It is deliberately small and
lenient: clients read the fields they need and ignore the rest, so the server can add fields
without breaking anyone. JSON, camelCase, ISO-8601 times, milliseconds for durations.

The Jellyfin-compatible surface (`jellyfin-compat.md`) exists only so request app, Sonarr and
Radarr can treat Roxy as a Jellyfin server. Roxy clients never use it (ADR-0007).

## Auth

| Method | Path | Body / result |
|---|---|---|
| POST | `/v1/auth/login` | `{username, password, client, device, deviceId, version}` -> `{token, user: {id, name, isAdmin}, deviceId}` |
| GET | `/v1/auth/me` | `{id, name, isAdmin}` or `{apiKey: true, isAdmin: true}` |
| POST | `/v1/auth/logout` | revokes the calling device token, 204 |

- The token is an opaque 64-hex device token, stored hashed. The same token store backs
  the compat surface, so a token minted here also works there and vice versa.
- Send it as `Authorization: Bearer <token>`. Media URLs that a player fetches on its own
  (libmpv cannot set headers reliably) carry it as `?api_key=<token>`.
- Integration API keys (minted through the compat `/Auth/Keys` for request app/arr) are accepted
  everywhere a device token is.
- Unauthenticated calls to anything but `system/info` and `auth/login` get `401 {"error": ...}`.

## Library

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/system/info` | `{name, version, ffmpeg}` |
| GET | `/v1/libraries` | `{libraries: [{id, name, kind, root, animeMode}]}` |
| POST | `/v1/libraries/scan` | async full rescan, 202 |
| GET | `/v1/items` | `{items: [Item], total}`; query below |
| GET | `/v1/items/{id}` | Item with `mediaSources` |
| GET | `/v1/sources/{id}` | MediaSource |

`/v1/items` query: `type=episode,movie,series,season` · `parent=<id>` · `recursive=1` ·
`library=<id>` · `sort=date_created|title|index` · `desc=1` · `limit` · `offset` ·
`include=sources` (attach `mediaSources` to each item).

Item: `{id, guid, libraryId, type, parentId?, title, sortTitle?, year?, overview?,
indexAired?, seasonAired?, indexAbsolute?, runtimeMs?, providerIds?, dateCreated,
dateModified, seriesId? + seriesTitle? (episodes, list views), mediaSources?}` plus the
metadata, `images` and `userData` fields described below.

MediaSource: `{id, itemId, name?, container?, size, durationMs, bitrate?, versionLabel?,
isPrimary, parts[], streams[], attachments[]}`. Streams carry `type` (`video|audio|subtitle`),
`codec`, `profile?`, `bitDepth?`, `width?`, `height?`, `fps?`, `rangeType?`
(`SDR|HDR10|HDR10+|HLG|DV`), `language?`, `detectedLanguage?`, `title?`, `isDefault`, `isForced`, `isExternal`,
`isTextFormat`. Attachments carry `index`, `filename`, `mime?`.

`detectedLanguage` (ISO 639-2/B) is sniffed from the text of subtitle tracks at ingest.
Clients should trust it over `language`: release groups mislabel tracks (two "Dialogue"
tracks both tagged `eng`, one of them Spanish, was found in the first real file tested).
It is absent for bitmap subtitles and for tracks too short to classify.

## Media

| Method | Path | Notes |
|---|---|---|
| GET/HEAD | `/v1/stream/{sourceId}` | the file, HTTP ranges honoured (`206`) |
| GET | `/v1/sources/{id}/attachments/{index}` | attachment bytes (fonts), extracted eagerly at scan |
| GET | `/v1/sources/{id}/subtitles/{index}` | text subtitle track, byte for byte |

## Metadata and images (Phase 1b contract)

Items gain, when a provider matched them: `overview`, `genres[]`, `tagline?`,
`communityRating?` (0-10), `officialRating?` ("TV-14"), `premiered?` (date), `status?`
(series: "Continuing" | "Ended" | "Upcoming"), `studio?`, `metadataRefreshedAt?`,
`providerIds` now including `imdb`, `anilist`, `mal`, `tvdb`, `tmdb`, and
`images: {poster?, backdrop?, logo?, thumb?}` whose values are relative URLs
`/v1/items/{id}/images/{type}`. Episodes get real titles, `overview`, `airDate?` (also
present as `premiered`) and a `thumb` still. Seasons get `poster`. Absent keys mean "no
image"; clients must render a placeholder and never 404-spam. The title of a matched
movie/series is the provider's (English where AniList has one), and a rescan does not
revert it.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/items/{id}/images/{type}` | `type` = poster, backdrop, logo, thumb. Served from the local cache (`data/cache/images/<item>/<type>.jpg`), `Cache-Control: public, max-age=86400`, 404 if none. Optional `?w=<px>` (16-4096, never enlarges) returns a resized JPEG cached as `<type>.w<px>.jpg`. Auth: Bearer or `?api_key=`. |
| POST | `/v1/items/{id}/refresh` | re-run metadata lookup for the item (its series when given a season/episode; children are refreshed with it), 202. 404 for an unknown item. |
| GET | `/v1/home` | `{rows: [{key, title, items: [Item]}]}` in display order: `continue` (resume list, empty for API keys), `recentEpisodes` (latest 20 episodes by date added, one per series, each with `seriesId` + `seriesTitle`; fetch the series poster via `/v1/items/{seriesId}/images/poster`), `recentMovies` (20), `series` (all, by date added, 50), `movies` (all, 50). Each item carries `images` and `userData`, so the home screen needs one request. |

Providers: TMDB for movies and series (API key via `ROXY_TMDB_API_KEY`; disabled with
one log line when unset), AniList for anime (no key): it leads when the library has
`animeMode`, otherwise runs after TMDB as fallback, and is always consulted so a match
carries its `anilist` id. Lookup runs asynchronously after ingest (and sweeps every
series/movie after a full scan, skipping anything refreshed within 7 days) and uses
`providerIds` from the filename/webhook first, title + year search second; a search hit
is accepted only when the normalized title is close enough, so unmatched names stay
unmatched. TheTVDB is not called; TVDB ids from Sonarr are resolved through TMDB's
`/find`. AniList episode titles/thumbs come from its `streamingEpisodes` for the matched
entry (season 1); multi-season anime get later seasons from TMDB only, for now.

## Missing and unaired episodes (placeholders)

After a series is matched, the server creates **virtual** episodes (and seasons) for
every episode the provider lists that the library has no file for, so clients can show
Jellyfin-style gaps. Specials (season 0) placeholders are only created when the library
already has a Specials season with a real file, because provider specials lists are long
and mostly recaps.

- `isVirtual: true` marks a placeholder; `hasMedia` says whether any media source exists.
- `availability` on episodes and movies: `available` | `missing` | `unaired` (air date in
  the future). Clients render missing/unaired rows greyed with a badge and must not try
  to play them (`/v1/stream` would 404).
- Placeholders share the scanner's GUIDs, so when the file arrives the same row flips to
  real: ids, watch state and images are kept.
- `GET /v1/items?...&hasMedia=1` hides placeholders; `hasMedia=0` lists only them (a
  "wanted" view). `/v1/home` rows only ever contain real items.
- `GET /v1/items?...&favorites=1` restricts to the caller's favourites (the client's
  Watchlist; 401 without a user login).
- `/v1/home` → `continue` lists **one entry per show** (the most recently played
  episode of each series, plus movies) and carries `mediaSources` so the client can show
  Dub | Sub and play without another round trip. `/v1/me/resume` applies the same rule.
- Logos are stored and served as PNG (transparency kept); other images are JPEG. The
  image endpoint's `Content-Type` follows the file.
- Rate limiting: the JSON API allows 20 requests/s sustained with a burst of 60 per
  client (IP + token), answering `429` with `Retry-After: 1` beyond that. Media bytes
  (`/v1/stream`, images, trickplay, attachments, subtitles) are exempt.
- Placeholders the provider stops listing are removed on the next refresh.

## Segments (skip intro / outro)

`MediaSource.segments[]`: `{type: intro | outro | recap | preview, startMs, endMs,
origin}`. Derived at ingest from the file's chapter markers (`origin: "chapters"`): among
55..125 s chapters starting within the first 4 minutes, the one closest to 90 s (the
length of nearly every TV anime OP/ED; a cold open can be 60..120 s too, so "first that
fits" is wrong) is the intro; the same rule over chapters starting in the last 5 minutes
gives the outro; a chapter of 45 s or less right after the outro is the preview. Movies (long chapters only) get none. `POST
/v1/libraries/scan?force=1` re-probes unchanged files, which is how existing items pick
segments up after a probe change. Planned origins: `fingerprint` (audio match across a
season for files without chapters) and `manual`.

## Trickplay (seek-preview thumbnails)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/sources/{id}/trickplay` | `200` manifest `{intervalMs, width, height, columns, rows, count, sheets, generatedAt}`; `202 {"status":"generating"}` after queueing (retry later); `404` unknown source |
| GET | `/v1/sources/{id}/trickplay/{n}` | sheet `n` (0-based), JPEG, immutable cache headers |

Generated server-side after ingest (one keyframe every 10 s, 320 px wide, 10×10 tiles
per sheet, `data/cache/trickplay/<source>/`) and swept for missing sources at startup.
Tile for time `t`: index `floor(t / intervalMs)`, sheet `index / (columns*rows)`, column
`(index % per_sheet) % columns`, row `(index % per_sheet) / columns`.

## Related titles (franchise view)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/items/{id}/related` | `{related: [RelatedEntry], partOf: [Item]}` |

`related` lists the provider's linked titles that are not seasons of this item: movies,
OVAs, side stories, spin-offs, alternative versions, summaries. Each entry has
`relation` (`movie | ova | special | side_story | spin_off | alternative | summary |
sequel | prequel | parent | other`), `provider` + `providerId`, `title`, `format`, `year`,
`inLibrary`, and either `item` (the library Item, with `userData`) or `posterUrl` (a
provider-hosted image for titles not in the library). `partOf` is the reverse link: the
library series whose relations include this item, so a movie page can show "Part of
KONOSUBA". Crossover relations (AniList `CHARACTER`) and source material are excluded.

Seasons: AniList models every TV season as its own entry; the server follows the
PREQUEL/SEQUEL chain so Season n maps to the n-th entry (`SeasonInfo.ProviderID`). When
AniList repeats one season's Crunchyroll episode list on another entry (a known data
problem), that season is marked untrusted and left to another provider (TMDB) to fill;
a forced refresh resets any wrong episode titles it wrote earlier. Numbering rule:
**the file's season/episode numbering is the truth**; the provider maps onto it, Specials
is only for files named `S00Exx`, and nothing is auto-moved.

## Watch state (native)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/me/resume?limit=` | `{items: [Item]}` with `userData.positionMs > 0` and not played, most recent first (default 20) |
| POST | `/v1/me/progress` | `{itemId, positionMs, audioIndex?, subtitleIndex?}` → 204. Marks played (and resets the position) at >= 90 % of runtime. 404 for an unknown item. |
| POST / DELETE | `/v1/me/played/{itemId}` | set / clear played, returns `{itemId, userData}` |
| POST / DELETE | `/v1/me/favorite/{itemId}` | set / clear favourite, returns `{itemId, userData}` |

Every Item in a list, detail, home or resume response for a user token carries
`userData: {positionMs, played, playCount, isFavorite, lastPlayedAt?, audioIndex?,
subtitleIndex?}` (one batched query per response). API-key callers get no `userData`,
and the `/v1/me/*` endpoints answer them 401. The same store backs the Jellyfin-compat
session endpoints, so progress reported either way is one record.

## Profiles

Several viewers on one login (Netflix-style). Watch state, favourites, `/v1/me/resume`
and `/v1/home` are **per profile**. Clients send `X-Roxy-Profile: <id>` on every request;
without it (or with a profile that is not the caller's) the user's default profile is
used, which is also what the Jellyfin-compat surface writes to. Migration 0007 gave every
existing user a default profile that inherited their history. At most 6 per account.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/me/profiles` | `{profiles: [Profile], current}`; `current` is the id this request resolved to |
| POST | `/v1/me/profiles` | `{name, avatarUrl?, avatarName?}` → 201 Profile |
| PATCH | `/v1/me/profiles/{id}` | `{name?, avatarUrl?, avatarName?}`; `avatarUrl: "-"` clears the picture |
| DELETE | `/v1/me/profiles/{id}` | 204; the last profile cannot be deleted, deleting the default promotes the oldest |
| GET | `/v1/me/profiles/{id}/avatar?w=` | the cached picture (JPEG), 404 when none |
| GET | `/v1/avatars/search?q=` | `{characters: [{id, name, image, series}]}` from AniList |
| GET | `/v1/avatars/suggested` | main cast of the library's series (up to 6 per show, cached an hour) |

`Profile: {id, name, isDefault, avatarName, hasAvatar, avatarUrl, createdAt}`. Avatars are
anime characters: `avatarUrl` on create/update must be an AniList CDN image (as returned
by the two `/v1/avatars` endpoints); the server caches a copy and serves it from
`/v1/me/profiles/{id}/avatar`. `/v1/auth/me` also returns `profileId`.

## Discover (requests; "our own request app")

TMDB through the server (the key stays there; posters are absolute `image.tmdb.org`
URLs the app loads directly). Every item carries `libraryItemId` when the library has
it (matched on the TMDB id) and `requestStatus` (`pending` | `sent` | `failed`) when
someone asked for it. Rows are cached 30 min, details 10 min, searches 5 min, and the
TMDB client has its own token bucket, so the app cannot hammer TMDB.

- `GET /v1/discover` → `{rows: [{key, title, items}], requestsEnabled: {tv, movie}}`.
  Rows: trending, new anime this season, popular anime, top rated anime, anime movies,
  popular movies, upcoming movies, popular series.
- `GET /v1/discover/search?q=` → `{items}` (TV and movies).
- `GET /v1/discover/{tv|movie}/{tmdbId}` → the item plus `tagline, status, seasons,
  episodes, runtimeMinutes, networks, tvdbId, imdbId, request, requestsEnabled`.
- `POST /v1/discover/request` `{type, tmdbId}` → the request row. Sent to Sonarr
  (by TVDB id, series type "anime" for anime) or Radarr (by TMDB id) when
  `ROXY_SONARR_URL` / `ROXY_SONARR_API_KEY` / `ROXY_RADARR_URL` / `ROXY_RADARR_API_KEY`
  are set (`ROXY_*_ROOT_FOLDER`, `ROXY_*_QUALITY_PROFILE` optional); otherwise kept as
  `pending`. 409 when the library already has it.
- `GET /v1/discover/browse?type=tv|movie&sort=popular|top|newest|oldest|votes&genres=16,35&lang=ja&anime=1&yearFrom=&yearTo=&minRating=&status=airing|ended&page=`
  → `{items, page, totalPages}` (TMDB discover; cached 5 min per query; `anime` =
  Animation + Japanese original language). `GET /v1/discover/genres?type=` → `{genres}`.
- `GET /v1/discover/requests` → `{items}` (everyone's, newest first).
- `POST /v1/discover/requests/retry` (admin) → resend pending requests now.

### Seasons and requests

A series detail carries `seasons: [{number, name, episodes, airDate, poster, have, known,
requested}]` (`have` = episodes with a file, `known` = files + placeholders). `POST
/v1/discover/request` takes `seasons: [n, …]` to ask for those only (empty = every season
the library lacks); a later request widens the existing one; seasons already complete are
refused. Sonarr gets per-season monitoring and a SeasonSearch per requested season (or
the seasons monitored on an already-added series). `requested_by` names the profile that
asked when the login is shared.

### Sonarr / Radarr settings (admin)

request app's Services model, saved on the server (settings key `arr`, migration 0010); the
`ROXY_SONARR_*` / `ROXY_RADARR_*` variables seed it when nothing is saved.

- `GET /v1/settings/arr` → `{sonarr, radarr, requestsEnabled}` with API keys masked as `••••••••`.
- `PUT /v1/settings/arr` `{sonarr: {url, apiKey, rootFolder, qualityProfileId,
  animeRootFolder, animeQualityProfileId, tags, animeTags, seasonFolders, searchOnAdd,
  externalUrl}, radarr: {url, apiKey, rootFolder, qualityProfileId, tags,
  minimumAvailability, searchOnAdd, externalUrl}}`; a masked key keeps the saved one.
  Saving rebuilds the clients and sends pending requests (also retried every 10 min).
- `POST /v1/settings/arr/test` `{kind, url, apiKey}` → `{version, appName, rootFolders,
  qualityProfiles, tags}` so the app can offer dropdowns.
- `DELETE /v1/discover/requests/{id}` → 204; the requester or an admin only.

## Webhooks (Sonarr / Radarr → Roxy)

`POST /v1/webhooks/sonarr` and `/v1/webhooks/radarr` (also `/v1/ingest/…`) take the arr
apps' Webhook connection payloads (`docs/api/arr-webhooks.md`). Auth: an API key as
`X-Api-Key` or `?api_key=`, or an admin token. Handled: `Download` (deleted files
dropped, new file ingested), `Rename` (old source dropped, new path ingested),
`EpisodeFileDelete` / `MovieFileDelete`, `SeriesDelete` / `MovieDelete` (everything
under the folder), `Test`; the rest are logged. Paths are the arr app's own: an explicit
`pathMappings: [{from, to}]` on the arr settings document is applied first, else the
longest tail of the path that exists under a library root (or, for removals, that the
library knows) wins. Every notification lands in `GET /v1/webhooks/events?limit=`
(admin) with its outcome. API keys: `GET/POST /v1/auth/keys`, `DELETE
/v1/auth/keys/{id}` (admin; the token is returned once on create).

## App updates

Self-hosted apps have no store, so Roxy Server is the update channel. After a build,
`apps/tv/tools/publish-apk.ps1 -Notes "..."` copies the APK into `<data>/apps/tv/` and
writes `latest.json` keyed by applicationId (debug and release tracked separately).

- `GET /v1/apps/tv/latest?package=<applicationId>` → `{versionCode, versionName, notes,
  publishedAt, size, url}`; 404 when nothing is published for that package.
- `GET /v1/apps/tv/download?package=<applicationId>` → the APK (Bearer or `api_key`).

The app checks once per launch (and at most every 6 h) after the session is validated,
compares `versionCode` with its own, and shows a bar above the bottom navigation:
Update downloads the APK to its cache and hands it to the system installer (Android asks
once to allow installs from Roxy); the close button hides that build until a newer one
appears.

## Not yet (see ROADMAP)

Playback decision (`/v1/items/{id}/playback`), device-code login, websocket events,
transcoding sessions.
