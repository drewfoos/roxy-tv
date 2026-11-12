# Roxy TV architecture

How Roxy is built today: the server, the Android app (phone and TV) and Roxy Web. This
document describes the code on `main` and is kept in step with it. Why each big choice was
made is in `docs/adr/`; the API is specified in `docs/api/native.md`; the dated history of
the work is in `docs/ROADMAP.md`.

Earlier drafts of this document described a Jellyfin-compatible API for other apps to
talk to. It was removed in ADR-0008; requests are handled by Roxy's own Discover
(section 4.10). Section 9 lists other ideas from those drafts
that were never built.

## 1. Goals and non-goals

### Goals

1. **Styled ASS subtitles render correctly everywhere.** Full libass fidelity (typesetting,
   signs, karaoke, the MKV's embedded fonts): libmpv on Android, jassub in the browser.
   Subtitles are never burned in or converted.
2. **Direct play first.** Hi10P H.264, HEVC Main10, FLAC, Opus, E-AC3 and TrueHD play
   untouched on the Android app. The client decodes; the server serves bytes.
3. **Re-encode only when asked.** A remux for browsers that cannot decode an audio track,
   and an H.264 transcode when the viewer picks a lower quality. Both keep the subtitles
   client-side.
4. **Accounts for a household.** Logins with several viewer profiles each, per-device
   tokens, TV sign-in by code or QR, and watch state per profile.
5. **Part of the arr pipeline.** Requests from Discover go straight to Sonarr and Radarr;
   their webhooks tell Roxy about imports, renames and deletes, with the IDs and
   episode numbers they already know.
6. **Anime-aware.** Episode numbering hints from Sonarr, AniList metadata, intro and outro
   detection, and help with the anime release habits that trip other servers (`v2` files,
   `(Season N)` batches, dubs that arrive weeks after the subs).
7. **Container-first.** One Docker image next to Sonarr and Radarr, with the same media
   mount (read-only) and GPU passthrough.

### Non-goals

- Live TV, DVR, DLNA, music, photos, books.
- SyncPlay / watch together.
- Jellyfin- or Plex-compatible APIs.
- Roku, webOS and Tizen apps.
- Ordered chapters / segment linking (FFmpeg cannot demux them; remux such releases). A file that links to others plays without those parts; the server reads its chapter editions itself (`probe/matroska.go`) so that a linked OP or ED is never taken for a part of the file and given a skip marker.

## 2. System context

```
   Roxy Discover ── request ──► Sonarr / Radarr ──► media folders
        │                          ▲   │
        │   season packs           │   │ webhook: import / upgrade / rename / delete,
        └──► Prowlarr ── push ─────┘   │          with series, season and episode
                                       ▼
                    +=============================================+
                    |                 Roxy Server                 |
                    |  scan / ingest ─ probe ─ metadata ─ serve   |
                    +======┬=======================┬==============+
                           │ direct play, remux,    │ direct play, remux,
                           ▼ transcode (Matroska)   ▼ transcode (Matroska)
                    +----------------+       +-------------+
                    | Roxy Android   |       |  Roxy Web   |
                    | phone and TV   |       |  React,     |
                    | libmpv+libass  |       |  jassub     |
                    +----------------+       +-------------+
```

Roxy never writes into the media folders. Everything it learns comes from scanning them,
the arr webhooks, and the metadata providers. Its own data (database, caches, extracted
fonts and subtitles, trickplay images) lives under `ROXY_DATA_DIR`.

## 3. Repository layout

```
roxy/
  server/                    Go module github.com/roxy-media/roxy/server
    cmd/roxy/                main: config, wiring, HTTP server
    cmd/arrsim/              a fake Sonarr/Radarr for local testing
    internal/
      api/                   the /v1 HTTP API (router.go lists every route)
      auth/                  passwords, device tokens, media tokens, API keys, throttling
      store/                 SQLite access; migrations/ holds the schema (embedded)
      library/               scanning, ingest, file naming, episode hints, sidecar subtitles
      probe/                 ffprobe wrapper, attachment and subtitle extraction
      ffmpeg/                locating and running the jellyfin-ffmpeg binaries
      metadata/              TMDB, AniList, fanart.tv, local .nfo files
      arrhook/               Sonarr/Radarr webhook receiver
      arr/                   Sonarr/Radarr/Prowlarr clients (add titles, search, packs)
      discover/              Discover: TMDB browsing, requests, download tracking
      transcode/             remux and transcode command lines, encoder detection
      segments/              intro / outro / preview detection
      trickplay/, stills/    seek-preview sprites; generated episode stills
      sessions/              "Play on": remote control of a signed-in player
      beacon/                LAN discovery (UDP)
      langdetect/            language of subtitle text (mislabelled tracks)
      netguard/, safefs/     guarded outbound fetches; the only file-writing path
      imaging/, model/, config/, web/ (Roxy Web, embedded)
  apps/
    android/                 one APK, phone and TV layouts (Kotlin, Compose, libmpv)
    web/                     Roxy Web (TypeScript, React, Vite, jassub)
  deploy/                    Dockerfile, docker-compose.yml (+ NVIDIA override), dev stack
  docs/                      this file, adr/, api/, design/, research/
```

One monorepo. The server and both clients change together; there is no other API
surface to keep stable.

## 4. Roxy Server

### 4.1 Stack

- **Go 1.27**, one static binary (`CGO_ENABLED=0`), `net/http` with the chi router.
- **SQLite** via `modernc.org/sqlite` (pure Go): WAL, foreign keys on, a 5 s busy
  timeout. Plain SQL migrations embedded from `internal/store/migrations`; queries are
  written by hand in `internal/store`.
- **jellyfin-ffmpeg** 8.1 (`ffprobe` and `ffmpeg`) run as subprocesses, never linked.
  It is used because its build bundles libass, VAAPI/AMF/QSV and the NVENC loader.
- Passwords are bcrypt; tokens are opaque random values stored as hashes.

See ADR-0002.

### 4.2 Data model

Four levels, as most media servers have them:

```
Library ──< Item (movie | series | season | episode) ──< MediaSource ──< MediaPart
                │                                            ├──< MediaStream
                ├──< ItemProviderId, ItemImage, ItemRelation  └──< Attachment (fonts)
                └── user data per profile, keyed by the item's GUID
```

| Tables | Role |
|---|---|
| `libraries` | The media folders (movies, series; anime flag), from `ROXY_MEDIA_ROOTS` or added in the app |
| `items`, `item_provider_ids`, `item_images`, `item_relations` | Titles and their tree. An item's `guid` (`tmdb:…`, `tvdb:…`, else a content hash) is its stable identity; aired, absolute and season numbers are separate columns |
| `media_sources`, `media_parts`, `media_streams`, `attachments` | Files, their tracks (codec, bit depth, HDR type, language, flags, `is_text_format`) and their fonts |
| `media_segments`, `segment_detection`, `audio_fingerprints` | Intro / outro / preview ranges and the data used to find them |
| `users`, `profiles`, `devices`, `api_keys`, `device_codes` | Logins, up to six viewer profiles each, signed-in devices (a token is a device row), webhook keys, TV sign-in codes |
| `user_item_data` | Position, played, play count, favourite (Watchlist), chosen tracks. **Keyed by profile and item GUID**, so a re-import or re-match keeps history |
| `departures`, `item_redirects` | Files that left the library in the last 30 days, matched against files that arrive so watch history follows a move (see §8), and old item ids mapped to their new items for clients that still hold them |
| `requests` | Discover requests and their state |
| `webhook_events`, `ingest_events` | What the arr apps sent, for the Webhooks page and debugging |
| `settings`, `series_characters*` | Admin settings (arr, TMDB, fanart.tv, transcoding); AniList characters for profile avatars |

`users.max_parental_rating`, `users.remote_bitrate_cap` and `user_library_access` exist in
the schema but nothing enforces them yet (section 9).

### 4.3 Ingest

Two ways in, both ending in the same per-file ingest:

- **Scanning.** Every library folder on start (`ROXY_SCAN_ON_START`, default on), and on
  demand from the app (scan, refresh, per-title refresh). File and folder names give the
  title, year, season and episode (`library/naming.go`; anime habits such as `S02E01v2`
  and absolute numbering included).
- **Arr webhooks** (`POST /v1/webhooks/sonarr|radarr`, API key in `X-Api-Key`; see
  `docs/api/arr-webhooks.md`). Imports and upgrades are ingested at once, renames
  re-ingested, deleted files and titles removed. Sonarr's season and episode numbers are
  kept as a hint for files whose names carry none, and survive later rescans. Paths are
  translated from the arr app's view of the disk to Roxy's.

Per file: probe (4.5), extract the attachments (fonts) and text subtitle tracks into the
cache **at ingest**, attach sidecar subtitles named the Bazarr way
(`<base>.<lang>[.forced][.hi|.sdh].<ext>`), then fetch metadata. There is no filesystem
watcher and no reconciliation poll: a file the arr apps did not report and that arrived
after the last scan appears with the next scan. Re-ingesting the same file is harmless.

### 4.4 Metadata

| Source | Role |
|---|---|
| TMDB | Movies and series: titles, overviews, images, seasons and episodes, ratings, cast, recommendations |
| AniList | Anime: titles, descriptions, characters (profile avatars), MAL and AniList links |
| fanart.tv (optional) | Logos and extra artwork |
| Local `.nfo` and artwork | Kodi / Jellyfin sidecars. An `.nfo` without ids (or with `<lockdata>`) is the metadata; one with ids steers the lookup, then its own fields win field by field. Episode `.nfo` files are always read (title, plot, ids, `<season>`/`<episode>`, multi-episode blocks, a special's `airsbefore`/`airsafter` place, which next-up follows). Artwork next to the media wins over fetched art (art picked by hand stays) |

Matching uses the IDs and names that ingest found; admins can fix a match (Identify),
refresh a title, pick other images or edit its metadata from the app. Outside images
are fetched through `netguard`, which refuses private addresses.

### 4.5 Probe

`ffprobe` at ingest, normalised into `media_streams`: HDR type from the colour data,
`is_text_format` true for ASS, SSA, SubRip, WebVTT and mov_text (false for PGS, DVD and
DVB bitmaps). Mislabelled subtitle languages are corrected by `langdetect` from the text
itself. Attachments are dumped with ffmpeg into the cache, so every client gets the
release's fonts.

### 4.6 Playback and streaming

There is no server-side playback decision. Each client knows what it can play and asks
for one of three streams; the server only serves them.

| Stream | Endpoint (`/v1`) | What it is |
|---|---|---|
| Direct play | `GET /stream/{id}` | The file itself, with range requests |
| Remux | `GET /sources/{id}/remux?audio=&start=` | Video copied, one chosen audio track (re-encoded to AAC only if a browser cannot decode it), timestamps from zero at the keyframe the seek to `start` lands on; Matroska on stdout. 8 slots |
| Remux start | `GET /sources/{id}/remux/start?start=` | `{startSec}`: where that remux's zero is in the file (the keyframe, up to a GOP before `start`); the player adds this to the stream clock |
| Transcode | `GET /sources/{id}/transcode?height=&bitrate=&start=` | H.264 at the chosen height and bitrate (tone-mapped to SDR for HDR), audio copied when compact, every subtitle track, font and chapter copied; Matroska on stdout. 4 slots |
| Subtitles | `GET /sources/{id}/subtitles/{index}` | An extracted text track, untouched (ASS stays ASS) |
| Fonts | `GET /sources/{id}/attachments/{index}` | An extracted attachment, served as a font only |
| Seek previews | `GET /sources/{id}/trickplay`, `/trickplay/{n}` | A manifest and sprite sheets |

Media URLs carry a short-lived **media token** for that device and source
(`GET /sources/{id}/token`), so a player never needs the device token in a URL; signing a
device out stops its streams. Remux and transcode write nothing to disk: ffmpeg's output
is the response, and a disconnect (stop, seek) kills it.

The encoder is chosen by test-encoding one second with NVENC, Quick Sync, VAAPI and AMF,
falling back to libx264; admins can pin one (Account > Server > Transcoding).
`ROXY_HWACCEL` is only the default.

Who asks for what:

- **Android:** always direct play through libmpv, unless the viewer picks a lower
  quality, which switches to the transcode at the same position.
- **Web:** direct play when the browser can decode the chosen audio track, the remux when
  it cannot (or another track is picked), the transcode for a lower quality.

### 4.7 Skip segments

Intro, outro and preview ranges per episode, found without anyone marking them: AniSkip
where it knows the show, otherwise Chromaprint fingerprints of the first and last minutes
of every episode in a season are compared to find the shared opening and ending. Clients
show a Skip button or skip automatically (a viewer preference).

### 4.8 Watch state

- `POST /v1/me/progress` every ~10 s and on pause and stop. When the ending starts (the
  version's outro segment: ED, credits) an item is marked played and its position reset;
  without a known ending, at 90 % of the runtime.
- Rewatching a played item past 30 s makes it "in progress" again at the new position
  (play count kept); a shorter peek leaves it played with no resume point. So an item is
  never both played and part-way.
- `GET /v1/home` builds Continue Watching (one entry per show: the episode under way, or
  the next one after the last finished) and the other rows; `/v1/me/history` and the
  Watchlist (`favorite`) come from the same table.
- Everything is per profile (`X-Roxy-Profile`).

### 4.9 Accounts and sign-in

- **First run:** the setup wizard creates the admin (from the LAN only).
- **Logins** with bcrypt passwords and sign-in throttling; each has up to six profiles with
  AniList-character avatars.
- **Devices:** every sign-in is a device with its own opaque token
  (`Authorization: Bearer`), idle-expiring after 180 days and revocable from the app.
  Roxy Web uses the same token in an HttpOnly session cookie.
- **TV sign-in:** the TV shows a code and a QR; a signed-in phone or browser approves it
  (`/v1/link/*`) and the TV receives its own device token.
- **API keys** are only for the arr webhooks and ingest endpoints.
- `X-Forwarded-For` is believed only from `ROXY_TRUSTED_PROXIES`.

### 4.10 Discover and requests

Roxy's own request flow. Discover browses and searches TMDB, marks what is
already in the library, and lets a profile request a movie, a show or single seasons.
Requests from non-admins wait for an admin's approval. Approved requests are added to
Radarr or Sonarr (configured root folder and quality profile, with optional separate
profiles for anime and for older anime), searching by season, and their download progress is tracked from the arr
queue. For finished seasons Roxy also searches Prowlarr for whole-season packs that Sonarr
cannot parse (`(Season N)`), scores them through Sonarr and pushes the best one, and every
12 hours looks again for seasons that only got subtitled releases, so a later dub is
picked up.

### 4.11 Other services

- **Play on** (`sessions`): a phone lists the players signed in to the same login and
  tells one to play something from a position; the TV plays it with its own player. A
  handover is confirmed by its own id once the TV's playback clock is running at the asked
  position, or fails back to the phone. Known limit: audio alone also runs that clock, so a
  file whose video the TV cannot decode but whose audio plays counts as handed over (left
  so on purpose, since audio-only playback is legitimate once music is supported).
- **LAN discovery** (`beacon`): UDP on 8096 answers "who is roxy?", so apps list servers on
  the network; `ROXY_ADVERTISE_URL` sets the address it gives out.
- **App updates:** the server hosts the Android APK and a `latest.json` (package, version,
  sha256); the app verifies the package, signing certificate and hash before installing.
- **Crash reports** from the apps (`/v1/diagnostics/crash`), and terms and privacy pages.

### 4.12 Configuration and operations

- Environment variables only (`internal/config`): `ROXY_LISTEN`, `ROXY_DATA_DIR`,
  `ROXY_MEDIA_ROOTS`, `ROXY_FFMPEG_PATH`, `ROXY_FFPROBE_PATH`, `ROXY_HWACCEL`,
  `ROXY_VAAPI_DEVICE`, `ROXY_SCAN_ON_START`, `ROXY_DISCOVERY_PORT`, `ROXY_ADVERTISE_URL`,
  `ROXY_OTHER_URLS` (more addresses of this server, e.g. its Tailscale one: the apps switch between them; optional, an admin can save the list in the app instead, and the variable wins when set),
  `ROXY_TRUSTED_PROXIES`, `ROXY_TMDB_API_KEY`, the Sonarr / Radarr / Prowlarr URLs, keys,
  quality profiles and root folders, `ROXY_ADMIN_USER` / `ROXY_ADMIN_PASSWORD`,
  `ROXY_DEBUG`. Most arr and provider settings can also be set from the app (admin).
- `GET /healthz` for health checks. Logs are structured (`slog`).

## 5. Roxy Android app

### 5.1 Stack

One APK for phones and TVs (`tv.roxy.tv`), choosing the phone or TV layout at start.
Kotlin, Jetpack Compose (Compose for TV on the TV), coroutines, OkHttp with
kotlinx.serialization for a hand-written API client (`api/RoxyApi.kt`), Coil for images,
DataStore for preferences, CameraX and ML Kit for the QR scanner, Timber for logs. Min SDK
28 (Android 9), target 36.

### 5.2 Player

One engine: **libmpv with libass**, from a self-built AAR (`app/libs/`). `PlayerSession`
drives it for both layouts.

- Decoding: MediaCodec zero-copy (`hwdec=auto`), copy mode as the fallback, software
  for what the hardware cannot do (Hi10P).
- Subtitles: libass, reading the fonts from the MKV itself, with a bundled fallback font
  set (`apps/android/FONTS.md`) for releases that do not carry theirs.
- Audio passthrough (AC3, E-AC3, DTS, TrueHD) when the TV or soundbar takes it; display
  refresh-rate switching to the video's frame rate; optional frame blending
  ("Smooth motion").
- Quality menu: Original (direct play) or a server transcode at 1080p, 720p or 480p.
- Phone extras: inline player on the episode page, picture-in-picture, downloads for
  offline viewing (progress is queued and replayed when the server is reachable again).
- TV extras: the launcher's Watch Next row, and a hold-OK menu on episodes and films
  (play from start, go to show, mark watched, file info).

### 5.3 Layout

```
apps/android/app/src/main/kotlin/tv/roxy/tv/
  api/          RoxyApi: the /v1 client and its models
  data/         preferences, item cache, server discovery, updates, downloads/
  player/       MpvPlayer (libmpv), PlayerSession, controls, trickplay, passthrough
  ui/           screens: phone/, tv/, login/, setup/, profiles/, settings/, components/
  tv/           Android TV launcher integration (Watch Next, images)
  diagnostics/  crash reports, device report
```

## 6. Roxy Web

TypeScript, React 19, react-router and Vite, built into the server binary
(`server/internal/web`). The player (`src/player/`) is a `<video>` element fed by the
direct, remux or transcode stream (section 4.6), with **jassub** (libass in WebAssembly)
drawing ASS/SSA from the extracted track and the release's fonts. It has the same pages as
the apps: Home, Browse, title pages, Discover and requests, Lists, account and admin.

Look and feel follow `docs/design/roxy-look.md` (Crunchyroll-like rows of cards, dark
theme); the TV app is the D-pad version of the same screens.

## 7. Deployment

The image is built from this repo (`deploy/Dockerfile`, `deploy/docker-compose.yml`); no
image is published to a registry yet.

- Multi-stage: Node builds Roxy Web, Go builds the server, the runtime is
  `debian:bookworm-slim` with the **jellyfin-ffmpeg** `.deb` (libass, libva with the
  radeonsi / iHD / i965 drivers, Quick Sync, AMF, the NVENC loader).
- Runs as a non-root user with `PUID` / `PGID`; the entrypoint joins the groups that own
  `/dev/dri/*`.
- Volumes: `/config` (`ROXY_DATA_DIR`: database and caches) and the media, mounted
  read-only.
- Ports: `8096/tcp` (HTTP) and `8096/udp` (LAN discovery). On a bridge network set
  `ROXY_ADVERTISE_URL` to the host's URL so the discovery reply points at the right place.
- GPUs: AMD and Intel pass `/dev/dri` (the compose default); NVIDIA uses
  `docker-compose.nvidia.yml` (`--gpus all`).
- CI (`.github/workflows/ci.yml`) runs `go vet`, `go test` and a build for the server, the
  type check and build for the web app, and the Android build.

## 8. Cross-cutting decisions

- **Stable GUIDs.** Watch state and URLs use an item's GUID, never its row id, so
  re-matching or re-importing keeps history.
- **History follows moved files.** A GUID comes from a file's path or the id tag in it,
  so a renamed folder, a move to another library or a re-encode makes a new item. A file
  whose record is removed is noted as a departure and matched against files that arrive,
  in any library and either order: the same bytes (size and extension), the same name
  and length under a title of the same name, or, once matched to a provider, the same
  film or the same series episode. The old item's watch state, Watchlist marks and
  hand-picked artwork move to the new one (merged if it has its own), up through season
  and series, and the old id redirects to the new one (`store/moves.go`).
- **Never convert subtitles.** ASS/SSA are served byte for byte; remux leaves them for the
  client, transcode copies them. No WebVTT, no burn-in.
- **Extract at ingest.** Fonts and text tracks are extracted when a file arrives, so no
  client ever plays a release without its fonts.
- **Clients own their capabilities.** No server-side device profiles or User-Agent rules.
- **One way to touch files** (`safefs`) and **one way to fetch from outside** (`netguard`).

## 9. Not built

Ideas from earlier drafts, kept here so they are not mistaken for features:

- A server-side playback decision engine (clients posting a capability profile).
- HLS streaming and Chromecast.
- Subtitle burn-in for clients without libass.
- A second Android engine (Media3/ExoPlayer).
- Parental rating caps, per-library access and a remote bitrate cap (schema columns
  exist, unused).
- Fallback fonts shipped in the image (`ROXY_FONTS_DIR` is read but not used yet).
- A filesystem watcher or a periodic reconciliation with Sonarr/Radarr.
- OIDC sign-in, Prometheus metrics, a WebSocket push channel.
- A published, multi-arch container image.

## 10. Risks

| Risk | Mitigation |
|---|---|
| A TV's chip cannot decode a file (Hi10P at high bitrates, AV1 on older SoCs) | The Quality menu falls back to a server transcode |
| The libmpv AAR is our own build, checked into `app/libs/` | Pinned by version in its file name; the build is written down in `apps/android/tools/build-libmpv.md`; libmpv and libass are long-lived upstream projects |
| Sonarr / Radarr API changes | All arr calls live in `internal/arr` (API v3); `cmd/arrsim` fakes both apps for local testing |
| Browser codec gaps (no AC3/E-AC3/DTS/TrueHD, limited Matroska) | The remux re-encodes only the audio; the transcode is H.264 |
| SQLite on large libraries | WAL, indices on provider IDs and paths; one writer |
| Scope creep into a full Jellyfin clone | The non-goals above |
