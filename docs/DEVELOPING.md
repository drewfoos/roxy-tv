# Building and running Roxy from source

For installing with Docker, see the [README](../README.md). This page is for working on the code.

## Repository map

One repository, three deliverables that version together:

| Path | What | Stack |
| --- | --- | --- |
| `server/` | **Roxy Server**: library, metadata, streaming, Discover, users; serves the `/v1` API and Roxy Web | Go 1.27, SQLite, jellyfin-ffmpeg |
| `apps/android/` | **Roxy Android app**: one APK with a phone layout and an Android TV layout | Kotlin, Jetpack Compose, libmpv |
| `apps/web/` | **Roxy Web**: the browser client for Windows, Linux and macOS, served by the server at `/` | TypeScript, React, Vite, jassub |
| `deploy/` | Dockerfile, Compose file, WSL firewall script | |
| `docs/` | architecture, roadmap, ADRs, API reference, design spec, research | |
| `testdata/` | scripts that generate the synthetic test media (the media itself is git-ignored) | |

`.github/workflows/ci.yml` vets and tests the server, builds the web app and assembles the
Android debug APK on every push.


## Running the server

Needs Go 1.27 and jellyfin-ffmpeg on the PATH (or `ROXY_FFMPEG_PATH` / `ROXY_FFPROBE_PATH`).

```
cd server
go build -o bin/roxy ./cmd/roxy
ROXY_MEDIA_ROOTS=/data/media ROXY_DATA_DIR=./data ROXY_LISTEN=0.0.0.0:8096 ./bin/roxy serve
```

A new server has no accounts: open the Roxy app or `http://<server>:8096/` from the same
network and the setup wizard creates the admin. Setup only answers from the home network
(the real connection; `X-Forwarded-For` counts only from `ROXY_TRUSTED_PROXIES`). To skip
the wizard, set `ROXY_ADMIN_PASSWORD` (and optionally `ROXY_ADMIN_USER`, default `roxy`)
and the admin is created at start; a weak password logs a warning. Put a TMDB key in `ROXY_TMDB_API_KEY` or in
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
their HTTP sweep (or use `network_mode: host`). Tested on WSL2 with NVENC, and in daily
use on Linux with VAAPI on an AMD RX 550. `deploy/pookiebox-deploy.sh` is the one-step
deploy for a Linux box that builds from this checkout: web build, `go vet` and tests,
Compose rebuild, then it waits for `/healthz`.

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
`versionCode`, then `apps/android/tools/publish-apk.sh` (or `publish-apk.ps1` on Windows)
copies the perf build and its compilation profiles to the server's `apps/tv` folder and
writes their sha256 into `latest.json`, so installed phones and TVs offer the update and
install it compiled (smooth from the first launch). `apps/android/FONTS.md` explains the fallback
font set and how to fetch it (fonts are not in the repository).


## Secrets and media

Nothing private is in the tree: API keys live in the data directory or the database,
test media and fonts are fetched or generated locally (`testdata/`), and `.gitignore`
blocks every media, font, key and local-config pattern. Each commit is grepped for keys
before it goes in; there is no git hook yet. Roxy is licensed under the GPL-3.0 (`LICENSE`).

## Naming

The project is **Roxy TV**. The backend is **Roxy Server** (`server/`), the Android client
is the **Roxy Android app** (`apps/android/`, phone and Android TV in one APK; its package
is still `tv.roxy.tv`), the browser client is **Roxy Web** (`apps/web/`). The bare name
`roxy` is taken by several reverse-proxy projects, so the namespace is `roxy-media`.
