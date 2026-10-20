# Roxy TV

A self-hosted media server and client pair built for anime: styled ASS subtitles rendered
on the device (libass, with the fonts from the MKV), 10-bit HEVC and Hi10P direct play,
household profiles, offline downloads, and a Discover tab that sends requests to Sonarr
and Radarr. It replaces Jellyfin for one household and keeps its port so the arr stack
only changes a host and a token.

```
you -> Roxy Discover -> Sonarr / Radarr -> download client -> /data/media -> Roxy Server -> Roxy TV app
```

## What works today (2026-10-20)

**Roxy Server** (`server/`, Go 1.27, SQLite, jellyfin-ffmpeg shelled out)

- Library scan of TRaSH-style folders (series/year/tvdb tag, SxxExx and absolute numbers,
  specials, sidecar subtitles), ffprobe of every file, eager font and attachment extraction,
  range-streamed direct play, subtitle serving with language sniffing for mislabelled tracks.
- Metadata from TMDB and AniList with a local image cache and on-the-fly resizing; AniList
  sequel chains become seasons; missing and unaired episodes show as placeholders; an art
  picker lets an admin choose any TMDB or fanart.tv banner, poster or logo, and the pick is
  locked against refreshes.
- Intro and outro markers from chapter markers, else by audio fingerprint across a season
  (Chromaprint, Intro Skipper's method); seek-preview thumbnails (trickplay).
- Users with device tokens, profiles with AniList character avatars, per-profile watch
  state, favourites, Continue Watching, a Home feed.
- Discover: trending, seasonal anime, search, filters, title pages with per-season and
  per-episode requests; requests go to Sonarr and Radarr when configured, else stay pending
  for the household. Sonarr and Radarr webhooks keep the library in step without a rescan.
- LAN discovery (a UDP beacon on 8096), in-app APK updates, server-served Terms of Use and
  Privacy, a Jellyfin-compatible subset for request app and the arr apps.

**Roxy TV app** (`apps/tv/`, Kotlin, Jetpack Compose, libmpv; minSdk 28; version 0.3.1)

- One APK with two layouts: a phone layout (portrait, Crunchyroll-style title pages, a
  Netflix-style hero carousel, bottom bar Home / Discover / Watchlist / Browse / Account)
  and a TV layout (landscape, D-pad).
- Player on libmpv with libass: MKV-attached fonts, a 50-font fallback set, hardware
  decode for HEVC 10-bit, 4K HDR10 and AV1, software decode for Hi10P. On the phone the
  video plays inline at the top of the episode or movie page; turn the phone for full
  screen, swipe home for picture-in-picture. Skip intro / outro, auto-skip, next-episode
  queue, trickplay scrubbing, pinch zoom, audio and subtitle language policy per profile.
- Offline downloads (Netflix model): per episode, per season, or a whole title, with
  resume, Wi-Fi-only, a storage check, offline playback and progress that syncs later.
- The login screen lists servers found on the network; tap one to fill the address.

Phone screens are verified on a Galaxy S25 Ultra after every change; the TV layout runs on
a TCL Q77K (Google TV). Roxy Web (`apps/web/`) is a placeholder with no code yet.

## Running the server

Needs Go 1.27 and jellyfin-ffmpeg on the PATH (or `ROXY_FFMPEG_PATH` / `ROXY_FFPROBE_PATH`).

```
cd server
go build -o bin/roxy ./cmd/roxy
ROXY_MEDIA_ROOTS=/data/media ROXY_DATA_DIR=./data ROXY_LISTEN=0.0.0.0:8096 ./bin/roxy serve
```

The first start creates an admin account (`ROXY_ADMIN_USER` / `ROXY_ADMIN_PASSWORD`,
default `roxy` / `roxy`: change it). Put a TMDB key in `ROXY_TMDB_API_KEY` or in
`<data dir>/tmdb.key`; a fanart.tv key, and the Sonarr and Radarr settings, are entered in
the app under Account → Server and stored in the database. Every setting is an environment
variable with a `ROXY_` prefix; `server/internal/config/config.go` lists them all. Port
8096 serves HTTP and the discovery beacon answers on 8096/udp.

`deploy/` has a Dockerfile and a reference Compose stack beside the arr apps (built, not
yet exercised on a real host).

## Building the app

Android Studio's JDK and the Gradle wrapper:

```
cd apps/tv
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

The debug build can prefill a server URL and login through `BuildConfig`; see
`apps/tv/app/build.gradle.kts`. `apps/tv/tools/publish-apk.ps1` copies a build to the
server so installed phones get the update bar. `apps/tv/FONTS.md` explains the fallback
font set and how to fetch it (fonts are not in the repository).

## Documents

- `docs/ARCHITECTURE.md`: the system design.
- `docs/ROADMAP.md`: phases, and a dated log of what has shipped.
- `docs/adr/`: architecture decision records.
- `docs/api/native.md`: the `/v1` API the Roxy clients speak.
- `docs/api/jellyfin-compat.md`: the Jellyfin-compatible subset for request app, Sonarr and Radarr.
- `docs/api/arr-webhooks.md`: the webhook payloads the server consumes.
- `docs/research/`: the research this design is based on, and the player spike results.

## Secrets and media

Nothing private is in the tree: API keys live in the data directory or the database,
test media and fonts are fetched or generated locally (`testdata/`), and a pre-push check
refuses commits that carry them. There is no LICENSE file yet; one is chosen before the
first public release.

## Naming

The project is **Roxy TV**. The backend is **Roxy Server**, the Android client is the
**Roxy TV app**, the browser client will be **Roxy Web**. The bare name `roxy` is taken
by several reverse-proxy projects, so the namespace is `roxy-media`.
