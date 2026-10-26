# Roxy Server

Go media server. Phase 2 MVP: filesystem ingest, ffprobe, eager font extraction,
direct-play streaming. See `../docs/ARCHITECTURE.md` section 4.

## Requirements

- Go 1.27+ (`~/tools/go` on the dev laptop; `go` on PATH)
- jellyfin-ffmpeg 8.1.x (`~/tools/jellyfin-ffmpeg` on the dev laptop, `/usr/lib/jellyfin-ffmpeg` in Docker).
  Found automatically; override with `ROXY_FFMPEG_PATH` / `ROXY_FFPROBE_PATH`.

## Run (dev, against the synthetic test media)

```powershell
cd server
go build -o bin\roxy.exe .\cmd\roxy

$env:ROXY_MEDIA_ROOTS = "..\testdata\out"   # comma separated; one library per root
$env:ROXY_DATA_DIR    = ".\data"            # db + caches (git-ignored)
$env:ROXY_LISTEN      = "0.0.0.0:8096"      # 0.0.0.0 so the phone on the LAN can reach it
$env:ROXY_DEBUG       = "1"
$env:ROXY_TMDB_API_KEY = "..."              # or put the key in data\tmdb.key; without one only AniList runs
.\bin\roxy.exe serve                         # scans on start, then serves
```

Other commands: `roxy scan` (scan and exit), `roxy probe <file>` (print the normalized probe).

## Endpoints (native API, v1)

| Method | Path | Purpose |
|---|---|---|
This is what Roxy's own clients speak (`../docs/api/native.md`). Everything below
`/v1` except `system/info` and `auth/login` requires `Authorization: Bearer <token>`;
media URLs the player fetches itself may carry the token as `?api_key=<token>` instead.

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | liveness |
| GET | `/v1/system/info` | name, version, ffmpeg version |
| POST | `/v1/auth/login` | `{username, password, client, device, deviceId, version}` -> `{token, user{id,name,isAdmin}, deviceId}` |
| GET | `/v1/auth/me` | the calling user |
| POST | `/v1/auth/logout` | revoke the calling device token |
| GET | `/v1/libraries` | libraries |
| POST | `/v1/libraries/scan` | rescan everything (async, 202) |
| GET | `/v1/discover`, `/v1/discover/search?q=`, `/v1/discover/{tv|movie}/{tmdbId}` | TMDB rows / search / title with library and request state (`docs/api/native.md` "Discover") |
| POST/GET/DELETE | `/v1/discover/request`, `/v1/discover/requests[/{id}]` | request a title (sent to Sonarr/Radarr; 412 while the matching one is not connected), list, withdraw |
| GET | `/v1/discover/browse?...`, `/v1/discover/genres?type=` | filtered TMDB browsing (type, sort, genres, language, anime, years, rating, status; paged) |
| GET/PUT/POST | `/v1/settings/arr`, `/v1/settings/arr/test` | admin: Sonarr / Radarr connection and defaults (request app-style), test connection lists root folders / profiles / tags |
| POST | `/v1/webhooks/sonarr`, `/v1/webhooks/radarr` | arr Webhook connections (API key): import / rename / delete without a rescan; `GET /v1/webhooks/events` (admin) |
| GET/POST/DELETE | `/v1/auth/keys[/{id}]` | admin: API keys for the arr apps |
| GET | `/v1/apps/tv/latest?package=` | newest published build of the app for that applicationId (`{versionCode, versionName, notes, size, url}`), from `data/apps/tv/latest.json` written by `apps/tv/tools/publish-apk.ps1` |
| GET | `/v1/apps/tv/download?package=` | that APK |
| POST | `/v1/segments/detect?season=&dry=1` | intro/outro detection by audio fingerprint for a season (report), or queue all seasons missing segments (202) |
| GET | `/v1/items?type=episode,movie&parent=&recursive=1&library=&sort=date_created|title|index&desc=1&limit=&offset=&include=sources` | list items; episodes carry `seriesId` + `seriesTitle`; every item carries `images` and (for a user token) `userData` |
| GET | `/v1/items/{id}` | item with media sources, streams, attachments, metadata, `images`, `userData` |
| GET | `/v1/items/{id}/images/{type}?w=` | cached poster / backdrop / logo / thumb as JPEG (`max-age=86400`); `?w=<px>` resizes and caches the variant; 404 when none |
| POST | `/v1/items/{id}/refresh` | force a metadata lookup (the series when given a season/episode), 202 |
| GET | `/v1/home` | `{rows: [{key, title, items}]}`: `continue`, `recentEpisodes` (one per series), `recentMovies`, `series`, `movies` |
| GET | `/v1/me/resume?limit=` | in-progress items for the calling user |
| POST | `/v1/me/progress` | `{itemId, positionMs, audioIndex?, subtitleIndex?}` -> 204; >= 90 % of runtime marks played |
| POST/DELETE | `/v1/me/played/{itemId}` | set / clear played -> `{itemId, userData}` |
| POST/DELETE | `/v1/me/favorite/{itemId}` | set / clear favourite -> `{itemId, userData}` |
| GET | `/v1/sources/{id}` | one media source |
| GET/HEAD | `/v1/stream/{id}` | direct play with HTTP ranges; `{id}` is a source id |
| GET | `/v1/sources/{id}/attachments/{index}` | extracted font / attachment |
| GET | `/v1/sources/{id}/subtitles/{index}` | text subtitle track, byte for byte (embedded tracks are extracted on first request; sidecars served directly) |
| GET | `/stream/{id}` | **unauthenticated dev convenience** for the player spike; `{id}` may be a file-name stem. Remove before release. |

## Jellyfin-compatible surface (implemented subset)

Mounted at the root so request app, Sonarr, Radarr and Jellyfin clients can point at Roxy
unchanged. Contract: `../docs/api/jellyfin-compat.md`.

- Public: `/System/Info/Public`, `/System/Ping`, `/Branding/Configuration`, `/Users/Public`,
  `POST /Users/AuthenticateByName`, `/QuickConnect/Enabled` (false for now),
  `/Videos/{id}/stream[.ext]`, `/Videos/{id}/{sourceId}/Attachments/{i}`,
  `/Videos/{id}/{sourceId}/Subtitles/{i}[/0]/Stream.{ext}` (token via `api_key`).
- Authenticated (device token or API key, via `Authorization: MediaBrowser ... Token=`,
  `X-MediaBrowser-Token`, `X-Emby-Token`, `Bearer`, or `?api_key=`): `/System/Info`,
  `/System/Configuration`, `/Users`, `/Users/Me`, `/Users/{id}`, `/Users/{id}/Views`,
  `/Auth/Keys` (POST/GET/DELETE), `/Library/MediaFolders`, `/Library/Media/Updated`,
  `/Library/Refresh`, `/Notifications/Admin`, `/Items` (incl. `ids=` detail form),
  `/Items/Latest`, `/Items/{id}`, `/Shows/{id}/Seasons`, `/Shows/{id}/Episodes`,
  `/Items/{id}/PlaybackInfo` (direct play only; ASS -> `External` when the profile
  declares it, else `Encode`), session reporting stubs (204), `/DisplayPreferences`.

`POST /v1/libraries/scan` and `POST /v1/segments/detect` are admin only.

First run creates an admin user from `ROXY_ADMIN_USER` / `ROXY_ADMIN_PASSWORD`
(defaults `roxy` / `roxy`; set them in production).

## Metadata and images

`internal/metadata` runs lookups off a queue fed by ingest: every ingested file enqueues
its series/movie, and a full scan re-enqueues every top-level item (anything refreshed
within 7 days is skipped unless `POST /v1/items/{id}/refresh` forces it). One worker
goroutine processes the queue; a bad item is logged, never fatal.

- **TMDB** (v3 key or v4 read token; resolved from `ROXY_TMDB_API_KEY`, else
  `<data dir>/tmdb.key`, else a project default injected at build time with
  `-ldflags "-X .../internal/config.DefaultTMDBAPIKey=<key>"` from a CI secret, so no key
  is ever committed): movies and series by provider
  id first (`tmdb` directly; `tvdb` / `imdb` from Sonarr/Radarr or `[tvdbid-…]` folder
  tags through `/find`), then title + year search. Seasons give episode titles,
  overviews, air dates and stills; absolute-numbered files are mapped through the
  season lengths. Token bucket ~35 req / 10 s, one retry on 429. Without a key the
  provider is disabled (one warning at start).
- **AniList** (no key, GraphQL): leads for anime-mode libraries, otherwise it runs
  after TMDB as fallback and always enriches a match with the `anilist` / `mal` ids.
  `streamingEpisodes` supply episode titles and thumbnails. AniList models each TV season
  as its own entry; the PREQUEL/SEQUEL chain is followed so Season n maps to the n-th
  entry, and its `relations` (movies, OVAs, side stories, spin-offs) are stored for
  `/v1/items/{id}/related`. A sequel entry that repeats another season's episode list
  (an AniList data problem) is marked untrusted and left to TMDB.
- Search hits are accepted only when the normalized title is close enough
  (`TitleSimilarity` >= 0.6, year as a tiebreaker), so junk names stay unmatched.
- Images download to `data/cache/images/<item>/<type>.jpg` (PNG re-encoded to JPEG) and
  are recorded in `item_images`; `?w=` variants are cached next to them.
- A match owns `title`, `year`, `overview` thereafter: rescans keep them (the item's
  `metadata_refreshed_at` is set), so Sonarr renames do not undo a lookup.

## Layout

```
cmd/roxy/            entry point and CLI
internal/config/     env-based configuration
internal/ffmpeg/     binary discovery + subprocess runner
internal/model/      domain types
internal/store/      SQLite (modernc, no cgo), embedded migrations, queries
internal/probe/      ffprobe normalization, attachment + subtitle extraction
internal/library/    ingest: scan, filename fallback parsing, sidecar subtitles
internal/metadata/   TMDB + AniList providers, lookup queue, image cache
internal/imaging/    box-filter resize for image variants (stdlib only)
internal/api/        chi router, v1 handlers, streaming, home, watch state
```

## Verified so far (2026-10-13, Windows dev laptop)

- Probe: Hi10P (H.264 High 10), HEVC Main10, AV1 10-bit, HDR10 transfer detection, FLAC/E-AC-3/Opus/AAC, ASS/SRT embedded, font attachments.
- Scan: items/seasons/series from folder + filename, Bazarr-style sidecars (`.en.ass`, `.en.forced.srt`), eager font extraction to `data/cache/attachments/<source>/`.
- Subtitle language sniffing: every text subtitle track is extracted at ingest into `data/cache/subtitles/<source>/` (which also warms the subtitle endpoint's cache) and classified by `internal/langdetect`; the result is `detectedLanguage` on the stream, and a mismatch with the container tag is logged. Caught a Spanish track tagged `eng` in the first real release tested.
- Movies inside a series-typed library: a file with no episode numbering and a year in its name or its immediate folder is a movie (`LooksLikeMovie`), and scene-style dotted names (`Title.2019.1080p.x265-GRP`) parse to title + year (`ParseMovieName`). Verified with real releases (Toy Story 5, KonoSuba Legend of Crimson); cover-art MJPEG streams are skipped by the probe.
- Streaming: `206 Partial Content` with correct `Content-Range` for head and mid-file ranges; attachment served byte-exact; ASS served unchanged.
- Metadata (2026-10-14, AniList only, no TMDB key): "Kono Subarashii Sekai ni Shukufuku o! Kurenai Densetsu" (2019 movie) matched KonoSuba: Legend of Crimson with overview, six genres, rating 8.2, premiered, studio, anilist + mal ids, poster (460x649) and backdrop; `?w=300` served a 300x423 JPEG. Synthetic names (`Roxy Test Show`, `test-hi10p-ass`, ...) stayed unmatched without errors. Migration 0003 upgraded the existing dev database in place.
- Watch state: `POST /v1/me/progress` -> `GET /v1/me/resume` round-trips position and track indexes; 95 % progress flips `played`; `/v1/home` carries `userData` on every item.

## Not yet

Quick Connect (device-code login), Sonarr/Radarr generic webhooks with IDs
(`docs/api/arr-webhooks.md`), AniList sequel chains for multi-season anime, logo images
from AniList, the full playback decision engine, transcoding, websocket. See
`../docs/ROADMAP.md`.
