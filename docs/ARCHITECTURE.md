# Roxy TV architecture

> **2026-10-26:** the Jellyfin-compatible API described below was removed (ADR-0008). Roxy's
> clients, Discover and the arr webhooks use the native `/v1` API; compat mentions here are history.

Status: draft v1, 2026-10-13. Based on the research in `docs/research/`.

## 1. Goals and non-goals

### Goals

1. **Styled ASS subtitles render correctly on the TV.** Full libass fidelity (typesetting,
   signs, karaoke, embedded MKV fonts) on Android TV, with no server transcode.
2. **Direct play first.** Hi10P H.264, HEVC Main10, AV1, FLAC/Opus/EAC3/TrueHD audio all
   play untouched. The client decodes; the server only serves bytes.
3. **Transcode only as a fallback**, with libass burn-in, for devices that cannot keep up.
4. **Per-user accounts** with device tokens, TV-friendly device-code login, watch state,
   library-scoped access, and parental caps.
5. **Drop-in for the arr pipeline.** request app, Sonarr and Radarr work unchanged by pointing
   them at Roxy Server as if it were Jellyfin.
6. **Anime-aware library.** Aired, absolute and AniDB numbering stored side by side;
   ingest keyed by TVDb/TMDb/AniDB IDs from Sonarr/Radarr, not filename regexes.
7. **Explainable playback decisions.** Every playback response says why it chose direct
   play, remux or transcode, per rule.
8. **Container-first deployment.** Roxy Server ships as a Docker image that slots into the
   existing compose stack next to Sonarr and Radarr, with the same
   `/data` mount and GPU device passthrough. Bare-metal is supported but secondary.

### Priorities

The player and its libass rendering are the reason the project exists. Everything else
is in service of that, so the build order is: player spike on the real TV, then the TV
app, then the server (which the player keeps working against Jellyfin until it exists).

### Non-goals (for v1)

- Live TV, DVR, DLNA, music, photos, books.
- SyncPlay / watch together.
- Plex-compatible API.
- Roku, webOS, Tizen native apps (they will be served burn-in via the web client later).
- Ordered chapters / segment linking. FFmpeg cannot demux them; remux such releases.

## 2. System context

```
   Roxy Discover ── request ──► Sonarr / Radarr ──► /data/media/{movies,tv}
                                      │
                                      │ webhook (import / rename / delete, with IDs)
                                      ▼
                    +=============================================+
                    |                 Roxy Server                 |
                    |  ingest ─ probe ─ metadata ─ decide ─ serve |
                    +======┬==================┬==================+
                           │ direct play /     │ transcode (slow connections)
                           ▼ range requests    ▼
                    +---------------+     +-------------+
                    | Roxy Android  |     |  Roxy Web   |
                    | libmpv+libass |     |  jassub     |
                    +---------------+     +-------------+
```

Every arrow into Roxy Server from Sonarr and Radarr is either a webhook or a poll Roxy
makes. Roxy never writes to `/data/media`; it only reads the library.

## 3. Repository layout

```
roxy/
  server/                 Go module: github.com/roxy-media/roxy/server
    cmd/roxy/             main: config, DI, HTTP server
    internal/
      store/              SQLite schema, migrations, sqlc queries
      library/            ingest pipeline, fs watcher, reconciliation
      probe/              ffprobe wrapper -> MediaStream/Attachment rows
      metadata/           provider interface + TMDB, TVDB, anime-lists, AniList
      playback/           decision engine (profile evaluation), stream URL builder
      stream/             direct play (range), remux, HLS transcode job manager
      ffmpeg/             argument builders per hwaccel vendor, process supervisor
      subtitle/           extraction cache, attachment extraction, font store
      session/            in-memory sessions, progress, websocket pushes
      auth/               users, device tokens, API keys, device-code login, OIDC (later)
      api/
        compat/           Jellyfin-compatible subset (request app/Sonarr/Radarr + TV app v1)
        v1/               Roxy native API (superset, explainable decisions, etc.)
      integrations/
        arr/              Sonarr/Radarr webhook receiver + /api/v3 reconciliation
    migrations/
  apps/
    android/              Android app, phone and TV layouts in one APK (Kotlin, Compose, libmpv)
    web/                  Roxy Web, the browser client (TypeScript, React, Vite, jassub);
                          its build is embedded in the server (server/internal/web)
  deploy/
    docker-compose.yml    Roxy Server
  docs/
```

Single monorepo. The server and the two clients version together; the compat API is
pinned to a documented Jellyfin API version (12.x shapes).

## 4. Roxy Server

### 4.1 Stack

- **Go 1.23+**, single static binary, `net/http` + chi router.
- **SQLite** via `modernc.org/sqlite` (pure Go, no cgo), WAL mode, foreign keys on,
  FTS5 for title search. Schema managed with `goose` migrations; queries via `sqlc`.
- **jellyfin-ffmpeg portable builds** (v8.1.x) invoked as subprocesses for `ffprobe`
  and `ffmpeg`. Never linked. Path configurable; the Docker image bundles it.
- **Fonts**: the image ships Noto Sans CJK and a configurable extra fonts dir. These
  feed libass burn-in and are exposed to clients as fallback fonts.

See ADR-0002 for the rationale.

### 4.2 Data model

Four-level media hierarchy, converged from Jellyfin and Plex:

```
Library (1) ──< Item (movie | series | season | episode) ──< MediaSource ──< MediaPart ──< MediaStream
                 │                                              │
                 ├──< ItemProviderId (source, value)            └──< Attachment (fonts)
                 ├──< ItemImage
                 ├──< Person / Genre / Tag (via join tables)
                 └──< UserItemData (per user, keyed by item GUID)
```

Core tables (abridged):

| Table | Key columns | Notes |
|---|---|---|
| `libraries` | id, name, kind (movies/series), paths[], anime_mode | One per Sonarr/Radarr root |
| `items` | id (uuid), guid (stable external key), library_id, type, parent_id, title, sort_title, year, overview, premiere_date, rating, index_aired, index_absolute, index_anidb, season_aired, season_anidb, runtime_ms, date_created, date_modified | `guid` is `tmdb:movie:123`, `tvdb:series:456`, `tvdb:episode:789`, falling back to a content hash |
| `item_provider_ids` | item_id, provider (tmdb/tvdb/imdb/anidb/anilist/mal), value | Indexed both directions |
| `media_sources` | id, item_id, name, path_root, container, size, duration_ms, bitrate, version_label, is_primary | One per file version (e.g. 1080p and 4K) |
| `media_parts` | id, source_id, index, path, size, duration_ms, inode, mtime | Multiple for stacked parts |
| `media_streams` | id, source_id, index, type (video/audio/subtitle), codec, profile, level, bit_depth, pix_fmt, width, height, fps, color_primaries, color_transfer, range_type (SDR/HDR10/HLG/DV/HDR10+), dv_profile, channels, channel_layout, sample_rate, language, title, is_default, is_forced, is_hearing_impaired, is_external, external_path, is_text_format | Mirrors Jellyfin's `MediaStream`; `is_text_format` is the subtitle classifier |
| `attachments` | id, source_id, index, filename, mime, extracted_path, extracted_at | **Extracted eagerly at ingest**, never lazily |
| `subtitle_cache` | source_id, stream_index, path, format, mtime | Extracted embedded text tracks |
| `users` | id, name, password_hash, is_admin, is_disabled, max_parental_rating, remote_bitrate_cap, subtitle_mode, subtitle_lang, audio_lang, remember_track_selection, created_at | |
| `user_library_access` | user_id, library_id | Library-scoped sharing |
| `devices` | id, user_id, name, client, client_version, device_id, token_hash, last_seen, capabilities_json | A token is a device row; revoke by delete |
| `api_keys` | id, name, token_hash, created_at | For request app/Sonarr/Radarr |
| `user_item_data` | user_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index | **Keyed by `item_guid`**, so re-matching preserves history |
| `device_codes` | code, secret, device_id, user_id (nullable until approved), expires_at | Quick-Connect-style login |
| `transcode_jobs` | id, session_id, source_id, params_json, state, started_at, last_ping | Also mirrored in memory |
| `ingest_events` | id, source (sonarr/radarr/fs/reconcile), payload_json, received_at, processed_at, error | Audit + replay |

Ordering is explicit: an episode stores `index_aired`/`season_aired`, `index_absolute`,
and `index_anidb`/`season_anidb` as separate columns. The library has a default display
order (aired, absolute, or anidb) and a user can override it per series.

### 4.3 Ingest pipeline

Primary source of truth is the Sonarr/Radarr webhook, which carries IDs and paths.
Filename parsing is the fallback for manual drops only. See ADR-0005 and
`docs/api/arr-webhooks.md`.

```
webhook (Download / Rename / *Delete)  ──┐
fs watcher (/data/media, debounced)    ──┼──► ingest queue ──► handler ──► tx:
reconciliation poll (Sonarr/Radarr v3) ──┘                                  upsert item by ID
POST /Library/Media/Updated (compat)   ──┘                                  upsert source/part
                                                                            probe (ffprobe JSON)
                                                                            extract attachments
                                                                            extract text subs
                                                                            resolve anime IDs
                                                                            enqueue metadata refresh
                                                                            publish LibraryChanged
```

Rules:

- **Idempotent.** Every handler keys on (`path`, `inode`, `mtime`, `size`). Re-delivering a
  webhook is harmless.
- **Upgrades are atomic.** A Sonarr `Download` with `deletedFiles[]` removes the old source
  and adds the new one in one transaction, so watch state (keyed by GUID) is untouched.
- **Rename never re-probes.** `renamedEpisodeFiles[]` re-keys paths in place.
- **Anime ID resolution.** Given `series.tvdbId` (+ season/episode) or `malIds`/`aniListIds`
  from Sonarr, look up Fribb/anime-lists (cached locally, refreshed weekly) to obtain
  `anidb_id`, `anilist_id`, `mal_id`, and the TVDb season/episode offset. Store all of them
  as provider IDs. request app reads `ProviderIds.AniDB` for its anime path.
- **Sidecars.** The fs watcher attaches `.ass/.srt/.ssa/.sup/.sub/.idx` files to the
  matching source using Bazarr's naming (`<base>.<lang>[.forced][.hi|.cc|.sdh][.tags].<ext>`).
- **Reconciliation** runs hourly against `/api/v3/series`, `/api/v3/episodefile`,
  `/api/v3/movie`, `/api/v3/moviefile` with the arr API keys and repairs anything missed.
- **Path mapping.** Roxy mounts `/data` at the same path as Sonarr/Radarr. A prefix map is
  available for setups that cannot.

### 4.4 Metadata

`metadata.Provider` interface: `Match(item) -> candidates`, `Fetch(ids) -> ItemMetadata`,
`Images(ids) -> []ImageRef`. Providers run out of process from the request path, in a
worker pool, rate limited per provider.

| Provider | Role |
|---|---|
| TMDB | Movies and series canonical metadata, images, season/episode numbering that request app expects |
| TVDB | Episode-level data for series Sonarr keyed by TVDb |
| anime-lists (Fribb) | ID mapping TVDb <-> AniDB <-> AniList <-> MAL, season/episode offsets |
| AniList (optional) | Anime titles, romaji/english/native, cover art, airing schedule |
| Local NFO (read-only) | Kodi/Jellyfin NFO sidecars if present |

Because Sonarr/Radarr already give us the right IDs, Roxy almost never has to *match*;
it only *fetches*. That removes the whole class of "wrong show" bugs.

### 4.5 Probe

`ffprobe -v quiet -print_format json -show_format -show_streams -show_chapters -show_frames
-read_intervals %+#1 -select_streams v:0` at ingest. Output is normalized into
`media_streams` rows, with:

- `range_type` derived from color transfer / side data (HDR10, HLG, DV with profile, HDR10+).
- `is_text_format` true for `ass`, `ssa`, `subrip`, `webvtt`, `mov_text`; false for
  `hdmv_pgs_subtitle`, `dvd_subtitle`, `dvb_subtitle`.
- Attachments (`codec_type == "attachment"`) recorded, then dumped with
  `ffmpeg -dump_attachment:t "" -i file -t 0 -f null -` into `cache/attachments/<source_id>/`.

### 4.6 Playback decision engine

Server-authoritative. The client posts a **versioned capability profile**; the server
evaluates each media source and returns a decision plus the reasons.

Profile schema (v1):

```jsonc
{
  "profileVersion": 1,
  "maxBitrate": 120000000,
  "directPlay": [                       // container x video x audio tuples
    { "containers": ["mkv","mp4","webm"], "video": ["h264","hevc","av1","vp9"], "audio": ["aac","flac","opus","eac3","ac3","truehd","dts","vorbis"] }
  ],
  "codecConditions": [                  // reject direct play when a condition fails
    { "codec": "h264", "bitDepth": { "max": 10 }, "level": { "max": 52 } },
    { "codec": "hevc", "bitDepth": { "max": 10 }, "rangeTypes": ["SDR","HDR10","HLG"] }
  ],
  "audio": { "maxChannels": 8, "passthrough": ["eac3","truehd","dts"] },
  "subtitles": [                        // first-class delivery axis per format
    { "format": "ass", "delivery": "native", "fetchesAttachments": true },
    { "format": "ssa", "delivery": "native", "fetchesAttachments": true },
    { "format": "subrip", "delivery": "native" },
    { "format": "pgs", "delivery": "native" }
  ],
  "transcodeTargets": [
    { "protocol": "hls", "container": "fmp4", "video": ["hevc","h264"], "audio": ["aac","eac3"], "maxChannels": 6 }
  ],
  "softwareDecode": { "h264_hi10p": true, "maxHeight": 1080 }   // client declares what it can do on CPU
}
```

Delivery values: `native` (client renders, fetching attachments if it says so),
`sidecar` (raw file download), `hlsText` (WebVTT segments; never for ASS/SSA),
`embed` (remux into MKV), `burn`.

Evaluation order per media source:

1. Direct play: container tuple matches, every codec condition passes, every chosen
   subtitle has a non-`burn` delivery. -> `DirectPlay` with a range-request URL.
2. Direct stream (remux): video passes, container or audio fails and a transcode target
   accepts video copy. -> `Remux` (HLS fMP4, `-c:v copy`, audio transcoded if needed).
   Subtitles still delivered natively or as sidecar if the client can render them. This is
   the "audio-only incompatibility with libass client" path that Plex never shipped.
3. Transcode: everything else. Subtitles burn in only if no non-`burn` delivery exists
   for the chosen track, or policy says always burn when transcoding.

Response includes `decision`, `reasons[]` (every rule evaluated and its outcome),
`streams` with `deliveryMethod` and `deliveryUrl` per subtitle, `attachments[]` with URLs,
and the `playSessionId`. The native API returns the full trace; the compat API maps it
onto Jellyfin's `PlaybackInfoResponse` with `TranscodeReasons`.

Policies (server-wide, overridable per user and per device): `burnWhenTranscoding`,
`burnComplexAss`, `preferRemuxOverTranscode`, `maxConcurrentBurnIn`, `remoteBitrateCap`.

### 4.7 Streaming

| Mode | Endpoint | Mechanism |
|---|---|---|
| Direct play | `GET /stream/{sourceId}` (+ compat `/Videos/{id}/stream`) | `http.ServeContent` with ranges; `ETag` from inode+mtime; no buffering in Go beyond kernel sendfile |
| Attachments | `GET /sources/{id}/attachments/{index}` (+ compat `/Videos/{id}/{sourceId}/Attachments/{index}`) | Serves pre-extracted fonts |
| Sidecar subs | `GET /sources/{id}/subtitles/{streamIndex}` (+ compat `/Videos/{id}/{sourceId}/Subtitles/{index}/Stream.{ext}`) | Serves extracted text track untouched; never converts SSA<->ASS or to VTT |
| Transcode for libmpv clients | `GET /v1/sources/{id}/transcode?height=&bitrate=&start=` | Matroska from ffmpeg's stdout, piped to the response; section 4.8 |
| Remux / transcode (planned, browsers and Cast) | `GET /hls/{sessionId}/master.m3u8`, `main.m3u8`, `seg/{n}.m4s` | Section 4.8 |

### 4.8 Transcoding

**Shipped (2026-10-20): the Matroska path for Roxy's own players.** The phone and TV play
through libmpv with libass, so they never need subtitles burned in or converted. When the
player's Quality menu asks for 1080p / 720p / 480p, the server
(`internal/transcode`, `api/transcode.go`) runs one jellyfin-ffmpeg per request:

- `-ss start -copyts -i file`, video re-encoded to H.264 (scaled, tone-mapped to SDR
  with `tonemapx` for HDR, capped at the requested bitrate), every subtitle track,
  attachment and chapter copied, `-f matroska pipe:1`. The response is that pipe; no
  files, no playlist, no job table. A disconnect (stop or seek) kills ffmpeg through the
  request context. Four slots; a fifth request gets 503.
- HLS/MPEG-TS cannot carry ASS or fonts, which is why this is Matroska: the player keeps
  libass rendering, dub/sub switching and skip markers exactly as in direct play.
- Audio is decided per track (copy compact AAC/AC3/E-AC3/Opus; FLAC/TrueHD/DTS to AAC,
  keeping 5.1; stereo AAC for 480p-class) so surround and soundbar passthrough survive.
- The encoder: a `Detector` test-encodes one second with NVENC, Quick Sync, VAAPI (Linux),
  AMF, else uses libx264, and caches the results. Admins pick Auto or one encoder in the
  app (Account -> Server -> Transcoding; `GET/PUT /v1/settings/transcoding`), saved in the
  settings table; `ROXY_HWACCEL` is only the default. NVIDIA decodes on the GPU too
  (`-hwaccel cuda`, falling back for Hi10P); the others decode on the CPU and upload.
- Player side (`PlayerSession`): the stream starts at the wanted position and keeps the
  file's clock (`rebase-start-time=no`); the duration comes from the source; a seek
  inside the buffer is local, anything else reloads the stream at the target; a reload
  that does not start in 20 s gives up with a message.

**Planned for browsers and Chromecast (Roxy Web, Cast): HLS.** Copy Jellyfin's HLS
design because it is correct:

- Synthesize the full VOD `main.m3u8` from duration (or keyframes when available) before
  starting ffmpeg.
- Serve segments lazily. A request for segment `n` either returns the file or restarts a
  single ffmpeg at `n * segmentLength` with `-ss`, `-copyts -avoid_negative_ts disabled`,
  `-f hls -hls_time N -hls_list_size 0 -start_number n`, fMP4 segments with
  `-hls_segment_type fmp4 -movflags +frag_discont+skip_sidx`.
- Kill and restart when the requested segment is behind the encoder or far ahead.
- Garbage-collect jobs after 60 s without a ping or progress report.
- Hardware chains built per vendor (VAAPI/AMF for the RX 550, QSV, NVENC, VideoToolbox),
  with the libass burn-in path using `alphasrc` + `subtitles=...:alpha=1:fontsdir=<attachments>`
  + `hwupload` + `overlay_<vendor>` so decoded frames stay on the GPU. `original_size` is
  passed so font scaling survives resolution changes.
- **Admission control**: burn-in jobs count against `maxConcurrentBurnIn` separately from
  GPU encode slots, because libass rasterization is always CPU-bound.

### 4.9 Sessions and watch state

- In-memory session map keyed by device token; not persisted (clients re-report).
- `POST /sessions/playing`, `/progress` (every 10 s and on pause/seek), `/stopped`, `/ping`.
- Progress writes `user_item_data` (position, played at >= 90% or configurable).
- WebSocket at `/ws` pushes `LibraryChanged`, `UserDataChanged`, `SessionEnded`, and
  (later) remote-control commands.
- Idle sessions dropped after 5 minutes.

### 4.10 Auth

- Local accounts, bcrypt/argon2 passwords, per-device opaque tokens (`devices.token_hash`).
- `Authorization: MediaBrowser Client=..., Device=..., DeviceId=..., Version=..., Token=...`
  accepted on the compat surface; `Authorization: Bearer <token>` on the native API.
- API keys for integrations (`/Auth/Keys` compat, `/v1/api-keys` native).
- **Device-code login** for TVs: the app shows a 6-character code, the user approves it
  on a phone or in Roxy Web, the TV polls and receives its device token. Wire-compatible
  with Jellyfin Quick Connect on the compat surface.
- Managed child profiles with PIN switch, parental rating caps, library scoping.
- OIDC federation is a later phase and is never required for playback.

### 4.11 API surfaces

1. **Compat (`/`, Jellyfin-shaped)**: the subset in `docs/api/jellyfin-compat.md`. Serves
   request app, Sonarr, Radarr, and the Roxy TV app v1 (ADR-0006). Shapes follow Jellyfin 12.x.
2. **Native (`/v1`)**: Roxy's own API. Superset: explainable decisions, ordering
   preferences, ingest events, policies, admin. OpenAPI generated from code; the TV and
   web clients generate their SDKs from it once they move off compat.

### 4.12 Configuration and operations

- Single `roxy.yaml` plus env overrides. Paths: `data_dir` (db, cache), `media_roots`,
  `ffmpeg_path`, `fonts_dir`, `hwaccel` (auto/vaapi/amf/qsv/nvenc/none), `arr[]` (url, api key).
- Health: `/healthz`; metrics: Prometheus `/metrics` (ingest lag, probe duration,
  active sessions, transcode slots, burn-in slots).
- Structured logs (slog JSON). Every playback decision logs its trace at debug.

## 5. Roxy TV app (Android TV)

### 5.1 Stack

Kotlin, Jetpack Compose for TV, Hilt, Coroutines, Coil, `jellyfin-sdk-kotlin` for the
compat API in v1 (replaced by a generated Roxy SDK later). Min SDK 28 (Android 9), target
Android TV 14. Builds for Android TV, Google TV, Fire TV.

### 5.2 Player engines

Two engines behind one `PlayerEngine` interface; chosen per file by a small rule set,
overridable per device and per playback.

| Engine | Use | Decode | Subtitles | Notes |
|---|---|---|---|---|
| **libmpv** (primary) | Anything with ASS/SSA; Hi10P; anything the Media3 path fails on | `hwdec=mediacodec-copy` with software fallback (FFmpeg) | libass built in; reads MKV attachments natively | Reference: mpv-android `MPVLib`, or jarnedemeulemeester's `libmpv-android` AAR. 20-40 MB native payload |
| **Media3/ExoPlayer** (secondary) | Plain H.264/HEVC with SRT/PGS or no subs; HDR/DV passthrough; tunneled playback | MediaCodec | `ass-media` libass overlay (OpenGL mode, raised glyph cache) for ASS; fonts fetched explicitly | Keeps Google's HDR and audio passthrough behaviour |

Rule of thumb for v1: **ASS present or Hi10P -> libmpv; otherwise Media3.** Measure on
the actual TV and adjust.

### 5.3 Capability profile

The app builds its profile at startup from `MediaCodecList` (hardware codecs, max
resolution, HDR types), audio passthrough probing, and a static declaration that
`h264` at bit depth 10 is supported via software decode up to 1080p. It declares
`ass`/`ssa`/`subrip`/`pgs` as `native` with `fetchesAttachments: true`. On the compat API
this is a Jellyfin `DeviceProfile` with `SubtitleProfiles` of `{Format:"ass", Method:"External"}`
and no bit-depth condition on H.264, which is what makes Jellyfin direct play Hi10P.

### 5.4 Font handling

- libmpv: pass the media URL; mpv reads attachments from the MKV stream itself. Also set
  `sub-fonts-dir` to the app's font cache, pre-populated with Noto CJK.
- Media3 + ass-media: download `attachments[]` from the playback response to the font
  cache before starting playback, then hand the directory to libass. Never assume the
  extension parses MKV attachments.

### 5.5 Runtime fallback

The engine reports dropped-frame rate. If sustained above threshold for 10 s during
software decode, the app re-requests playback with `forceTranscode: true` (compat:
re-POST `PlaybackInfo` with a profile that omits the software-decode claim) and switches
to the HLS stream at the same position. This is the one feature that makes the app
strictly better than Kodi on weak hardware.

### 5.6 App structure

```
apps/android/
  app/                 Compose for TV screens: Home, Library, Series, Season, Player, Login (device code), Settings
  core/api/            backend adapter interface + Jellyfin-compat implementation (+ Roxy native later)
  core/player/         PlayerEngine interface, MpvEngine, Media3Engine, engine selector
  core/profile/        capability probing, profile builder
  core/fonts/          font cache, attachment downloader
  core/data/           repositories, Room cache for offline browsing of the library
```

## 6. Roxy Web

TypeScript + Vite + React, HLS.js for transcoded streams, native `<video>` for direct
play of browser-compatible containers (MP4/WebM), **jassub** for ASS rendering (WebGL,
embedded-font support, actively maintained), fonts from the attachments endpoint plus a
server fallback-font list capped in size. Tizen/webOS browsers are tested, not promised.

**Look and feel: Netflix-style.** Dark theme, a full-bleed hero banner at the top of
Home (backdrop, logo/title, synopsis, Play/Resume and Info actions), horizontal rows
below it ("Continue watching", "Recently added", "Next up", per-library and per-genre
rows) with landscape cards that scale on hover/focus, a details page with a backdrop
hero, season tabs and an episode list with thumbnails, and a top nav bar that fades
on scroll. The same visual language is used by the Roxy TV app so the two feel like one
product; the TV app is the DPAD-focus version of the same screens. Browsing a library
should never show a grid of posters as the primary view; rows first, grid as a secondary
"see all".

## 7. Deployment (Docker first)

Roxy Server is built and tested as a container from the first commit. The reference
stack lives in `deploy/docker-compose.yml` and `deploy/Dockerfile`.

Image design:

- Multi-stage build: Go build stage (Go 1.27) -> runtime stage on `debian:bookworm-slim`.
- Runtime stage installs the **jellyfin-ffmpeg** `.deb`, which bundles libass, libva with
  the radeonsi / iHD / i965 drivers, Quick Sync, AMF and the NVENC loader; no separate
  driver packages are needed. About 550 MB.
- Runs as a non-root user with `PUID`/`PGID` (linuxserver-style) so files in `/config`
  keep the owner's UID; the entrypoint adds the user to whatever groups own `/dev/dri/*`.
- `ROXY_DATA_DIR=/config`. Volumes: `/config` (db, cache, extracted attachments and
  subtitles), `/data` (media, read-only), optional `/fonts` (extra fonts). Transcodes
  stream from ffmpeg's stdout and write nothing to disk.
- Ports: `8096` (keep Jellyfin's port so request app/arr configs only change host and token)
  and `8096/udp` for LAN discovery (the beacon answers "who is roxy?"; `ROXY_DISCOVERY_PORT`,
  0 to turn off). On a bridge network the reply carries the container's address, so set
  `ROXY_ADVERTISE_URL` to the host's URL, or the clients' HTTP sweep finds it anyway.
- Healthcheck: `GET /healthz`.
- Published as `ghcr.io/roxy-media/roxy-server:<semver>` and `:latest`, multi-arch
  (`linux/amd64`, `linux/arm64`), from CI on every tag.
- GPUs: AMD/Intel pass `/dev/dri` (the compose default, for the RX 550); NVIDIA uses the
  same image with `--gpus all` (`deploy/docker-compose.nvidia.yml`; the image sets
  `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility`). Tested 2026-10-20 in Docker on
  WSL2 with a GTX 1650: NVENC about 6.5x realtime for 1080p HEVC to 720p, software x264
  about 2x. VAAPI on real AMD hardware is still to be checked on the server.

Conventions (TRaSH): `/data/media/{movies,tv}` for the library, Roxy mounts `/data`
read-only. The Sonarr/Radarr webhook URLs point at the `roxy` service name on the
compose network.

Development loop: `docker compose -f deploy/docker-compose.dev.yml up` mounts the source,
runs `air` for hot reload, and shares the same `/data` mount as the production stack.

## 8. Cross-cutting decisions

- **Stable GUIDs for everything user-facing.** Items are addressed by `guid` in user data
  and in URLs exposed to clients; the row id is internal. Re-matching never loses history.
- **Never convert subtitle formats.** SSA/ASS are served byte-for-byte. VTT conversion only
  ever happens for SRT, and only for `hlsText` delivery.
- **Eager extraction.** Attachments and text tracks are extracted at ingest, so burn-in and
  clients always find fonts. This is the single most common Jellyfin anime bug, removed.
- **No server-side profile registry by User-Agent.** Clients own their profile; the server
  validates and evaluates it. Plex's registry rotted; Jellyfin's model is right.
- **Ship fonts.** Noto CJK in the image, served to clients as fallback fonts.

## 9. Risks

| Risk | Mitigation |
|---|---|
| libmpv on the specific Costco TV SoC stutters with hwdec | Try `mediacodec-copy`, then software decode; keep Media3 engine; runtime fallback to transcode |
| Jellyfin compat API drift (12.x removed legacy prefixes; future majors may change shapes) | Pin to 12.x shapes; integration tests replay real request app/Sonarr/Radarr requests |
| request app never marks seasons AVAILABLE for anime | Expose seasons with TMDb numbering and `ProviderIds.AniDB`; test with real anime series |
| ass-media single maintainer | libmpv is the primary engine; ass-media is the hedge |
| Go SQLite performance on large libraries | WAL, prepared statements, FTS5, indices on provider ids and paths; benchmark at 50k items |
| Scope creep into a full Jellyfin clone | Non-goals list; ROADMAP exit criteria per phase |
