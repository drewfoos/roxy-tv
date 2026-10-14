# Roxy TV roadmap

## Status (2026-10-13)

Hardware (Jellyfin server, Costco TV) is in transit, so work started on what needs
neither: synthetic test media and the server MVP. Phase 0/1a wait for the TV; the spike
APK can be sideloaded on the owner's Android phone meanwhile.

- Done: research, architecture docs, ADRs, API contracts, Docker files (untested),
  Android TV spike source (unbuilt: no Android Studio on the laptop yet),
  `testdata/make-test-media.ps1` (5 files incl. Hi10P, HEVC10, HDR10 4K, AV1, dual audio,
  Bazarr sidecars), Roxy Server MVP in Go: scan, probe, eager font extraction, direct-play
  range streaming, attachments, subtitle serving. All verified against the test media.
- Also done (later on 2026-10-13): Jellyfin-compatible subset (system, libraries, items,
  seasons/episodes, PlaybackInfo, media URLs), users + device tokens + API keys with the
  MediaBrowser auth scheme, `/Library/Media/Updated` ingest, TRaSH folder parsing verified
  (series/year/tvdb tag, seasons, SxxExx + absolute numbers, episode titles, sidecars).
  Android SDK installed headlessly (API 36/37, build-tools 37), Gradle 9.8 wrapper in place.
- Also done (2026-10-13, evening): persisted watch state on the compat surface. **Spike
  APK ran on the owner's phone (Galaxy S25 Ultra)**: libmpv + libass rendered the ASS
  test with MKV-attached fonts correctly in all 15 runs of `apps/tv/tools/spike-matrix.ps1`;
  Hi10P decodes in software, HEVC10 / 4K HDR10 / AV1 10-bit in hardware. 2026-10-14: test
  set grown to 10 files (H.264 8-bit, HEVC 60 fps + 5.1, Blu-ray TrueHD + PGS via
  `testdata/tools/make-pgs.py`, HLG, MP4); all 30 runs pass, PGS renders. Two real
  releases (KonoSuba movie, Toy Story 5) play and seek; a mislabelled subtitle track led
  to server-side subtitle language sniffing (`detectedLanguage`) and a title-aware track
  policy in the player. The app now bundles a 50-font fallback set (`apps/tv/FONTS.md`)
  with fontconfig aliases, fixed `FONTCONFIG_FILE` wiring, and colour-emoji rejection;
  `test-symbols-fallback.mkv` renders every symbol/emoji/CJK line with no boxes. Results:
  `docs/research/player-spike-SM-S938U.md`. Decision (ADR-0007): Roxy clients speak the
  native `/v1` API, the Jellyfin SDK was removed from the app, and the server gained
  native login + Bearer/`api_key` auth on `/v1` (`docs/api/native.md`).
- Also done (2026-10-14, night): **Phase 1b has started.** Server: TMDB + AniList metadata
  with a local image cache and `?w=` resizing, `/v1/home`, native watch state
  (`/v1/me/*`), Jellyfin-style **missing/unaired episode placeholders** (`isVirtual`,
  `availability`) with Specials handled as season 0. App (`apps/tv`, package
  `tv.roxy.tv`): login, Netflix-style home (hero + rows), series page with season row,
  episode list with thumbs, titles, progress and greyed "Missing" rows, movie page,
  settings, player with progress reporting. Verified on the phone against the real
  KonoSuba S1+S2+OVA release (Cerberus) with one episode held back: AniList matched,
  stills and titles filled, E5 shown as Missing, and the placeholder flipped to real when
  the file was put back. Screens: `apps/tv/spike-runs/20261014-missing-episodes/`.
- Also done (2026-10-14, later): the user's corrections. OVAs stay where the release numbers
  them (KonoSuba OVA = S01E11; Specials only for `S00Exx` files). AniList sequel chains
  are followed (Season n = n-th SEQUEL entry), relations are stored and exposed at
  `/v1/items/{id}/related`, and the app shows a Crunchyroll-style **Movies & OVAs** row on
  the series page plus a **Part of: <series>** button on the movie page (verified:
  Legend of Crimson ↔ KONOSUBA). Found and handled an AniList data bug (KonoSuba 2 carries
  Season 1's Crunchyroll episode list): repeated lists are refused, a forced refresh resets
  wrong titles. Orphaned rows are pruned when files move. Still needed: **a TMDB key**
  (user to register; embed as project default like Jellyfin) for Season 2 titles, episode
  descriptions and non-anime posters.
- Next on the server: playback decision endpoint,
  Quick Connect / device-code login, generic Sonarr/Radarr webhooks with IDs,
  metadata/images, websocket, Docker image build test.
- Next on the client: run the same matrix on the Costco TV when it arrives (the
  performance decision needs it), then start Phase 1b on the native API.

The ordering follows one principle from the research: **the client is where the payoff
is, and direct-play-first makes the server small.** Because the Costco TV already decodes
Hi10P and renders ASS in Kodi, Roxy Server's MVP needs no transcoder at all.

## Phase 0 — Baseline on the current stack (days)

Goal: confirm the client-side approach on the actual TV before writing code.

- Upgrade Jellyfin to 12.2; mount Noto CJK fonts into the container.
- Install Wholphin or the Jellyfin Android TV 0.20 beta (developer libass toggle) on the TV.
- Verify: Hi10P + ASS episode direct plays with styled subs; note CPU/dropped frames.
- Record which SoC the TV has (`Settings > About`), since it fixes the libmpv hwdec plan.

Exit: a written note in `docs/research/` of what the TV handles natively.

## Phase 1a — Player spike (1-2 weeks) — START HERE

Goal: prove libmpv + libass on the actual TV before building any UI or server code.
This is the single biggest unknown in the project and the thing it exists for.

- Throwaway Android TV app: hardcoded Jellyfin URL and token, one screen listing the
  last 20 episodes, a player screen.
- Player: libmpv (via `libmpv-android` AAR or mpv-android's `MPVLib`), fed the Jellyfin
  direct-play URL. Options to test in order: `hwdec=mediacodec-copy`, `hwdec=mediacodec`,
  `hwdec=no`. `sub-fonts-dir` set to the app font cache with Noto CJK.
- Test set: a Hi10P x264 episode with heavy typesetting, a HEVC Main10 episode with
  karaoke, a 4K HEVC HDR10 episode, a dual-audio FLAC episode.
- Measure and write down: dropped frames (`mpv` stats overlay), CPU, whether MKV font
  attachments are picked up, HDR output, audio passthrough.
- If libmpv stutters on this SoC: try Media3 + `ass-media` (OpenGL mode) on the same
  files as the comparison, and decide the default engine from data, not assumption.

Exit: a short results note in `docs/research/player-spike-<tv-model>.md` and a decision
on the default engine and hwdec mode for this TV.

## Phase 1b — Roxy TV app v1 against Roxy Server (3-6 months, one developer)

Goal: a daily-driver Android TV client with Kodi-grade playback and a Netflix-style UI.
Backend: Roxy Server's native `/v1` API only (ADR-0007; the earlier plan to run against
Jellyfin 12.2 through the Jellyfin SDK is superseded).

- Compose for TV skeleton: device-code login, Home, Library, Series,
  Season, Episode, Player, Settings.
- `PlayerEngine` interface, **libmpv engine first**, Media3 engine second, using the
  spike's findings.
- Capability profile builder; ASS declared `External`; H.264 without bit-depth condition.
- Font cache with Noto CJK; attachment download for the Media3 path.
- Playback reporting loop (`Sessions/Playing`, `Progress`, `Stopped`).
- Runtime dropped-frame fallback to transcode.
- Device test matrix: the Costco TV, plus at least one Fire TV Stick or Onn box.

Exit: every ASS/Hi10P episode in the library plays direct with styled subs on the TV;
watch state syncs with Jellyfin; request app/arr untouched.

## Phase 2 — Roxy Server MVP, direct play only (3-4 months)

Goal: replace Jellyfin for direct-play clients while request app, Sonarr and Radarr see "Jellyfin".

- Go skeleton, config, SQLite schema + migrations, OpenAPI for `/v1`.
- **Dockerfile and compose from the first commit** (`deploy/`): multi-arch image with
  jellyfin-ffmpeg, Noto CJK, VAAPI drivers, PUID/PGID, `/dev/dri` passthrough, healthcheck;
  dev compose with hot reload sharing the same `/data` mount. CI builds and pushes to GHCR
  on every tag.
- Ingest: Sonarr/Radarr webhook receiver, `/Library/Media/Updated`, fs watcher, hourly
  reconciliation, anime-lists ID resolution.
- Probe + eager attachment/subtitle extraction.
- Metadata: TMDB + TVDB fetchers (no matching needed), images, local NFO read.
- Users, devices, device-code login, API keys, library scoping.
- Decision engine with `DirectPlay` and `Remux-not-available-yet` paths; explainable reasons.
- Direct play streaming with ranges, attachments, sidecar subtitles.
- Sessions, progress, websocket.
- Compat API subset (`docs/api/jellyfin-compat.md`) with replay tests from captured
  request app/Sonarr/Radarr traffic.
- Run Roxy Server in parallel with Jellyfin; point a second request app instance at it.

Exit: request app imports users and marks requests available via Roxy; Sonarr/Radarr
notifications land; the Roxy TV app works unchanged pointed at Roxy Server; the whole
thing runs from `docker compose up` next to the existing containers.

## Phase 3 — Transcoding fallback (2-3 months)

- HLS job manager (playlist-first, lazy segments, restart on seek, ping GC).
- ffmpeg argument builders: software, VAAPI/AMF (RX 550), QSV, NVENC.
- libass burn-in chain with admission control; `original_size`.
- Remux path (video copy + audio transcode + native subs).
- Policies: burn-when-transcoding, burn-complex-ASS, concurrency caps.

Exit: a phone on cellular and a weak Fire Stick both play any file through Roxy.

## Phase 4 — Roxy Web (2-3 months)

- React + Vite, jassub, HLS.js, fallback fonts, device-code approval page, admin UI
  (libraries, users, policies, ingest events, decision traces).

Exit: Jellyfin can be turned off.

## Phase 5 — Later

- Native Roxy API in the TV app (explainable decisions UI, ordering preference).
- Android mobile variant of the TV app.
- OIDC federation.
- Swiftfin-style Apple TV client via VLCKit, if demand exists.
- Trickplay thumbnails, intro/credit skip markers, collections.

## Explicitly deferred

- Plex API compatibility, live TV, DLNA, SyncPlay, music.
- Ordered chapters (FFmpeg limitation; remux releases instead).
