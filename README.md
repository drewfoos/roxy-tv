# Roxy TV

A self-hosted media server and client pair built for anime: styled ASS subtitles rendered
on the device (libass, with the fonts from the MKV), 10-bit HEVC and Hi10P direct play,
household profiles, offline downloads, and a Discover tab that sends requests to Sonarr
and Radarr. It replaces Jellyfin for one household and keeps its port so the arr stack
only changes a host and a token.

```
you -> Roxy Discover -> Sonarr / Radarr -> download client -> /data/media -> Roxy Server -> Roxy apps
```

![Roxy TV's Home on an Android TV: the hero for The Legend of Korra with Continue: S4 E13, the side menu, and Recently added](docs/images/tv-home.jpg)

*Roxy TV on a TCL Google TV: Home, with the show you are watching in the hero and the side menu (Back opens it).*

## Repository map

One repository, three deliverables that version together:

| Path | What | Stack |
| --- | --- | --- |
| `server/` | **Roxy Server**: library, metadata, streaming, Discover, users; serves the `/v1` API and Roxy Web | Go 1.27, SQLite, jellyfin-ffmpeg |
| `apps/android/` | **Roxy Android app**: one APK with a phone layout and an Android TV layout | Kotlin, Jetpack Compose, libmpv |
| `apps/web/` | **Roxy Web**: the browser client for Windows, Linux and macOS, served by the server at `/` | TypeScript, React, Vite, jassub |
| `deploy/` | Dockerfile, Compose stack beside the arr apps, WSL firewall script | |
| `docs/` | architecture, roadmap, ADRs, API reference, design spec, research | |
| `testdata/` | scripts that generate the synthetic test media (the media itself is git-ignored) | |

`.github/workflows/ci.yml` vets and tests the server, builds the web app and assembles the
Android debug APK on every push.

## What works today (2026-11-02)

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
  per-episode requests sent to Sonarr and Radarr, with live download status (progress,
  time left, stalled, failed with the reason, nothing found yet) and Search again / Try
  another release; until Sonarr and Radarr are connected the app offers to set them up. Sonarr and Radarr webhooks keep the library in step without a rescan.
- Transcoding for lower qualities: H.264 in Matroska with every subtitle track, font and
  chapter kept, audio chosen per track (5.1 kept where it can be), on NVIDIA NVENC, Intel
  Quick Sync, VAAPI, AMD AMF or the CPU. Auto picks the best one; an admin can choose in
  the app. Runs in Docker with the GPU passed through.
- LAN discovery (a UDP beacon on 8096), in-app APK updates, server-served Terms of Use and
  Privacy. (The Jellyfin-compatible API was removed; see ADR-0008.)
- Requests go straight to Sonarr and Radarr (no request app). They are offered only once Sonarr
  or Radarr has a root folder and a quality profile chosen in the settings (an address and
  key alone, say from the environment, are not enough). Requests follow a hand-tuned arr
  setup rather than fighting it:
  - Anime goes to its own root folder and profile (series and films); anime are added as
    series type "standard" by default, with a setting for "anime". An optional older-anime
    profile catches shows that first aired before 2010 and have ended, which often exist
    only as DVD or 480p releases.
  - New series are searched season by season, so season packs are found; Sonarr's own
    search on add (episode by episode) is never used, because the one stray episode it
    grabs blocks the pack.
  - A show Sonarr has but does not monitor is treated as kept by hand: the request is
    refused rather than turning on downloads for the whole series.
  - A finished import shows up in seconds, from the webhook or from the request tracker
    noticing the new files.
- Saving settings from an older app never wipes a setting it does not know about.
- Sign-in by phone: a TV or browser shows a QR code and a six-letter code, and a signed-in
  phone approves it. The sign-in page's poster wall comes from the server's own library
  (`/v1/showcase`, public, posters only).
- Profile avatars: "From your library" characters are saved per series by a slow background
  worker (AniList for anime; TMDB's cast for live action; none for other animation), so the
  editor opens instantly and a thousand new shows are looked up over hours, not at once.

**Roxy Android app** (`apps/android/`, Kotlin, Jetpack Compose, libmpv; minSdk 28; version 0.5.20)

- One APK with two layouts, shown on the launcher as **Roxy TV**: a phone layout (portrait,
  Crunchyroll-style title pages, a Netflix-style hero carousel, bottom bar Home / Discover /
  Browse / My Lists / Account) and a TV layout (landscape, D-pad).
- On the TV: a side menu (Search, Home, My Lists, Discover, Browse, Settings; Back opens it
  from any page and returns to where you were), My Lists with Watchlist and History, Browse
  by category and genre, Discover with requests, a TV profile editor, and a sign-in screen
  that finds servers on the network and signs in with a phone's camera. Updates published
  on the server are installed from the side menu.
- Player on libmpv with libass: MKV-attached fonts, a 50-font fallback set, hardware
  decode for HEVC 10-bit, 4K HDR10 and AV1, software decode for Hi10P. On the phone the
  video plays inline at the top of the episode or movie page; turn the phone for full
  screen, swipe home for picture-in-picture. Skip intro / outro, auto-skip, next-episode
  queue, trickplay scrubbing, pinch zoom, audio and subtitle language policy per profile.
  A Quality menu (Original, 1080p, 720p, 480p) switches to a server transcode for slow
  connections; the TV app sends Dolby and DTS untouched to a soundbar when it finds one.
- Offline downloads (Netflix model): per episode, per season, or a whole title, with
  resume, Wi-Fi-only, a storage check, offline playback and progress that syncs later.
- The login screen lists servers found on the network; tap one to fill the address.
- Browse on the phone: categories (Newly Added, A-Z, Series, Movies, genres) as chips over a
  poster grid with the title and year under each poster.

Phone screens are verified on a Galaxy S25 Ultra after every change; the TV layout runs on
a TCL Q77K (Google TV).

**Roxy Web** (`apps/web/`, TypeScript, React, Vite; started 2026-10-26)

- The computer client: Crunchyroll's layout at desktop width (`docs/design/roxy-look.md`),
  the same `/v1` API as the Android app, served by the server itself at `/`.
- Sign-in over a poster wall (password, or approve from a phone), "Who's watching?" as on
  the apps, Browse with a category list and genres, Discover with title pages, request
  status and download progress, and the Sonarr / Radarr settings for admins.
- Playback is the browser's own decoder with no transcoding: MKV, H.264 (8-bit and
  Hi10P), HEVC 10-bit (GPU), AV1; ASS/SSA rendered by jassub (libass in WebAssembly)
  with the fonts from the MKV. Verified in Chrome on every file in the test library,
  including the KonoSuba opening and ending karaoke. Chrome and Edge are the targets;
  Firefox cannot play MKV or Hi10P.

## Running the server

Needs Go 1.27 and jellyfin-ffmpeg on the PATH (or `ROXY_FFMPEG_PATH` / `ROXY_FFPROBE_PATH`).

```
cd server
go build -o bin/roxy ./cmd/roxy
ROXY_MEDIA_ROOTS=/data/media ROXY_DATA_DIR=./data ROXY_LISTEN=0.0.0.0:8096 ./bin/roxy serve
```

The first start creates an admin account named `ROXY_ADMIN_USER` (default `roxy`) with
the password in `ROXY_ADMIN_PASSWORD`; leave it unset and a random password is generated
and printed once in the server log. Put a TMDB key in `ROXY_TMDB_API_KEY` or in
`<data dir>/tmdb.key`; a fanart.tv key, and the Sonarr and Radarr settings, are entered in
the app under Account → Server and stored in the database. Every setting is an environment
variable with a `ROXY_` prefix; `server/internal/config/config.go` lists them all. Port
8096 serves HTTP and the discovery beacon answers on 8096/udp.

Transcoding uses the best GPU encoder it finds (`ROXY_HWACCEL=auto`); an admin can pick
another in the app under Account -> Server -> Transcoding, which is saved and wins over
the variable. `ROXY_VAAPI_DEVICE` picks the render node on Linux (default
`/dev/dri/renderD128`).

### Docker

`deploy/` has the Dockerfile and a Compose stack beside the arr apps. The image carries
jellyfin-ffmpeg with the GPU drivers it needs; the database and caches go in `/config`.

```
cd deploy
ROXY_ADMIN_PASSWORD='something-strong' docker compose up -d --build roxy               # AMD / Intel (/dev/dri)
docker compose -f docker-compose.yml -f docker-compose.nvidia.yml up -d --build roxy   # NVIDIA
docker logs roxy 2>&1 | grep "transcode: encoder"
```

NVIDIA needs the nvidia-container-toolkit on the host (`nvidia-ctk runtime configure
--runtime=docker`). Use `"8097:8096"` while Jellyfin still owns 8096. Docker's port
mapping does not pass UDP broadcasts, so on a bridge network the apps find the server by
their HTTP sweep (or use `network_mode: host`). Tested on WSL2 with NVENC; VAAPI on an
AMD card is next.

### On Windows (WSL2)

Smart App Control can block freshly built unsigned `roxy.exe` files, so the dev server
runs under WSL2 (Ubuntu 24.04) with `networkingMode=mirrored` in
`%USERPROFILE%\.wslconfig`, which keeps the PC's LAN address. WSL's Hyper-V firewall
blocks inbound LAN traffic by default: run `deploy/wsl-firewall.ps1` once in an admin
PowerShell to open 8096 (TCP and UDP). NVENC works inside WSL with the normal Windows
NVIDIA driver.

## Building Roxy Web

Node 24 and npm. The build lands in `server/internal/web/dist/`, which the server embeds,
so `go build` after `npm run build` gives one binary that serves the UI at `/`.

```
cd apps/web
npm ci
npm run dev      # Vite on http://127.0.0.1:5173, /v1 proxied to the server on 8096
npm run build    # production bundle into server/internal/web/dist
```

## Building the Android app

Android Studio's JDK and the Gradle wrapper:

```
cd apps/android
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

The debug build can prefill a server URL and login through `BuildConfig`; see
`apps/android/app/build.gradle.kts`. For a TV, build the `perf` variant (`./gradlew
assemblePerf`): the debug app's id, key and sign-in, built the release way. A plain debug
build is debuggable, so Android never compiles it ahead of time and a TV feels slow. Bump
`versionCode`, then `apps/android/tools/publish-apk.ps1` (perf by default) copies the build to
the server's `apps/tv` folder so installed phones and TVs offer the update. `apps/android/FONTS.md` explains the fallback
font set and how to fetch it (fonts are not in the repository).

## Documents

- `docs/ARCHITECTURE.md`: the system design.
- `docs/ROADMAP.md`: phases, and a dated log of what has shipped.
- `docs/adr/`: architecture decision records.
- `docs/api/native.md`: the `/v1` API the Roxy clients speak.
- `docs/adr/0008-remove-jellyfin-compat-and-harden-auth.md`: why the Jellyfin API went, and how sign-in and media URLs are protected.
- `docs/api/arr-webhooks.md`: the webhook payloads the server consumes.
- `docs/research/`: the research this design is based on, and the player spike results.

## Secrets and media

Nothing private is in the tree: API keys live in the data directory or the database,
test media and fonts are fetched or generated locally (`testdata/`), and `.gitignore`
blocks every media, font, key and local-config pattern. Each commit is grepped for keys
before it goes in; there is no git hook yet. There is no LICENSE file yet; one is chosen before the
first public release.

## Naming

The project is **Roxy TV**. The backend is **Roxy Server** (`server/`), the Android client
is the **Roxy Android app** (`apps/android/`, phone and Android TV in one APK; its package
is still `tv.roxy.tv`), the browser client is **Roxy Web** (`apps/web/`). The bare name
`roxy` is taken by several reverse-proxy projects, so the namespace is `roxy-media`.
