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
- Placeholders the provider stops listing are removed on the next refresh.

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

## Not yet (see ROADMAP)

Playback decision (`/v1/items/{id}/playback`), device-code login, websocket events,
transcoding sessions.
