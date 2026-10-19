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
  wrong titles. Orphaned rows are pruned when files move. **TMDB key registered** by the
  owner and kept out of the tree (`data/tmdb.key` locally; CI injects a project default
  via ldflags for release images): Season 2 titles and episode descriptions, Season 3 as a
  missing season, Toy Story and Legend of Crimson posters/logos/taglines all verified on
  the phone. Source is on GitHub: private `drewfoos/roxy-tv`.
- Also done (2026-10-14, early hours): server-side **trickplay** thumbnails (10 s
  keyframes, tiled sheets, ~9 s per episode), and the player got a **YouTube-layout
  control bar** with scrubbing preview, Autoplay switch, CC toggle, Settings sheet,
  picture-in-picture, mute and fill. All verified on the phone (PiP via button and via
  Home). Screens in `apps/tv/spike-runs/20261014-controls/`.
- Also done (2026-10-14, 03:30): owner feedback round on the player. Seek bar redrawn by
  hand (full width, red from 0:00, round thumb), trickplay preview aspect fixed, Settings
  is now a YouTube-style popup that closes on tap-away, Autoplay switch removed (always
  on), and **Skip intro / Skip outro** with an **auto-skip** option: the server detects
  OP/ED from chapter markers (`media_segments`, migration 0006; 22 of 36 episodes in the
  test library have them) and the app shows the pill or jumps automatically. All verified
  on the phone. The TV is a TCL 65" Q77K (Google TV), so the same APK sideloads.
- Also done (2026-10-14, 04:00): "Ends at" clock, double-tap seek, OP detection picks
  the chapter closest to 90 s, pinch zoom v2 (YouTube-style: persistent level with a
  "1.5×" readout, 0.1× steps, one mpv update per frame; the owner still has to try it by
  hand, adb cannot inject two fingers), centre buttons as plain dark discs.
- Also done (2026-10-14, 04:30): **phone layout**. The app now picks a layout at start:
  TV = landscape D-pad screens as before; phone = portrait, edge-to-edge, Netflix-style
  (hero poster card with Play/Info, poster rows, bottom bar Home / Series / Movies /
  Settings, Netflix title pages with season chips and episode rows). Navigation moved to
  Navigation Compose with typed routes. Verified on the S25 Ultra, including play and
  return. Screens in `apps/tv/spike-runs/20261014-controls/5*.png`.
- Also done (2026-10-14, 07:20): phone layout round two from the owner's feedback. Hero is
  a full-bleed Crunchyroll-style carousel (poster bleeding under logo, "★ 8.5 Dub | Sub •
  genres", 3-line synopsis, pill Start watching + save circle, story-style timer bars,
  8 s dwell; selection = continue-watching shows, then newest arrivals, then library),
  pinned header (witch-hat mark, Cast, Search) that turns black on scroll, compact bottom
  bar Home / Watchlist / Browse / Account, shimmer skeletons everywhere instead of
  "Loading…", plain fade transitions, season **dropdown**, watched ticks, My List /
  Watchlist (favourites), Search, Account page with "coming soon" rows for profiles,
  notifications, email, password, ToS, privacy. Server: one Continue Watching entry per
  show, `favorites=1` filter, PNG logos, per-client rate limit; app caps concurrent
  requests. PiP uses auto-enter (seamless swipe-home on Android 12+).
- Also done (2026-10-14, 07:40): **profiles**. Migration 0007 adds `profiles` and re-keys
  watch state by profile (every user got a default profile with their history). Clients
  send `X-Roxy-Profile`; `/v1/me/profiles` CRUD, cached avatars, and avatar pickers from
  AniList (`/v1/avatars/suggested` = main cast of the library's series, `/search?q=`).
  App: "Who's watching?" at launch with Add / Manage, an editor with "From your library"
  and character search, Edit / Switch profile on Account, the avatar in the bottom bar.
  Verified on the phone: a new profile starts with no Continue Watching. TV uses the same
  picker (no focus highlight yet). Launcher icon and TV banner are the witch hat.
- Also done (2026-10-19): **downloads** (Netflix model). Per-episode control on episode rows,
  "Download Season N · size" under the season dropdown, Download on movies, "Download all
  episodes" in every card's three-dot menu. A data-sync foreground service fetches one video
  at a time with HTTP-range resume (pause / resume / cancel, Wi-Fi-only default, progress
  notification), plus poster/backdrop/logo/thumb and Bazarr sidecars. Downloads page (Account,
  and Home when the server is away: hat + sonar rings + "Your downloads" row) grouped by
  show with a storage bar. Downloaded items play from disk even at home; segments and sniffed
  subtitle languages come from the snapshot; positions watched offline queue and sync on the
  next launch; "Who's watching?" uses a cached profile list. Home shows the offline state
  within 3 s (probe beside the request). Watched episodes get a full red bar. Verified on the
  S25 Ultra: KonoSuba S1 (11 episodes, 2.8 GB) downloaded, paused, resumed, played offline,
  progress synced. Not yet: smart downloads (delete watched / fetch next), downloads on TV.
- Also done (2026-10-19, later): **next-episode queue**. The player resolves the episode after
  the current one (server order; downloads only when offline), shows the Netflix end card at
  the outro marker when the credits end the file, at the `preview` marker when something
  follows the credits (post-credits scene: Skip outro handles the credits), else the last 15 s, with a 10 s countdown, Play now / Watch credits, and a
  Next button in the control bar; the next file loads on the same mpv instance (no activity
  restart). Moving on marks the finished episode watched (queued offline). "Autoplay next
  episode" switch under Account > Playback. Verified on the S25 Ultra: S01E03 → E04 → E05.
- Also done (2026-10-19, night): **intro/outro detection by audio fingerprint** for files
  without chapter markers (`server/internal/segments`, Intro Skipper's method): Chromaprint
  via jellyfin-ffmpeg of the first 10 and last 6 minutes per episode (cached in
  `audio_fingerprints`, migration 0008), pairwise longest shared run with neighbours
  (Hamming ≤ 6, gaps ≤ 3.5 s), silence-snapped edges, preview = what follows the ED
  (5..60 s). Runs per season 20 s after ingest settles and at start-up for seasons missing
  segments; `POST /v1/segments/detect?season=&dry=1` reports. Graded against the 22
  chaptered KonoSuba episodes: 22/22 OPs and EDs found, edges within ~1 s of the markers
  on most, a few seconds on EDs that fade in under the last shot. ~2 s per episode the
  first time (fingerprints), ~1 s after. Not yet: recap detection, black-frame credits
  fallback for live action, a per-series "no OP" opt-out.
- Also done (2026-10-19, late): **in-app updates**. The server hosts the newest APK per
  applicationId (`data/apps/tv/latest.json` + file, written by `apps/tv/tools/publish-apk.ps1`;
  `GET /v1/apps/tv/latest|download`). The app checks after login and shows a bar above the
  bottom navigation ("Roxy 0.3.1 is available · notes · Update · ×"), downloads with
  progress, then opens the package installer (REQUEST_INSTALL_PACKAGES + FileProvider).
  Verified on the S25 Ultra: 0.2.0 → 0.3.0 installed through the bar. Phone layout only
  so far (the TV needs a focusable version).
- Also done (2026-10-19, night): **Discover tab** ("our own request app"). Server
  `internal/discover` over the existing TMDB client: trending, new anime this season,
  popular / top-rated anime, anime movies, popular / upcoming movies, popular series;
  search; title detail; every item marked when the library has it (TMDB id match) or
  someone requested it. Requests (migration 0009) go to Sonarr / Radarr through
  `internal/arr` when `ROXY_SONARR_*` / `ROXY_RADARR_*` are set (untested until the
  server machine exists), else stay pending for the household to see. App: Discover is
  the second tab (search box, "Your requests" row, poster rows with badges), a title
  page with Watch / Request / Requested + Withdraw. Hardening pass the same night:
  TMDB detail and search caches, request body limits, admin-only scan and detect,
  withdraw restricted to the requester or an admin, strict file names on the app-update
  endpoint, sanitised ids in download paths, and the Bearer token is only sent to the
  Roxy host (TMDB images go bare).
- Also done (2026-10-20, early): **Discover filters and arr settings**. Filter bar under the
  search box (Series / Movies, Anime, Sort, Filters · n, Clear) and a bottom sheet (sort,
  genres, original language, years, rating, status) over `GET /v1/discover/browse`
  (TMDB discover, paged, infinite scroll, cached 5 min per query). Admin page Account →
  Server → Sonarr & Radarr, laid out like request app's Services page: URL, API key, Test
  connection (lists root folders, quality profiles, tags from the server), root folder /
  quality profile pickers, anime root folder / profile / tags for Sonarr, minimum
  availability for Radarr, season folders, search on add. Saved on the server (settings
  table, migration 0010); pending requests are sent on save and every 10 min. The owner's
  arr stack lives on their server, so the live add calls stay untested until deploy day.
- Also done (2026-10-20): **arr webhooks** (`server/internal/arrhook`): Download / Rename /
  file and title deletes keep the library in step without a rescan; arr-side paths are
  mapped by explicit mappings or by the longest tail under a library root; outcomes are
  logged (`webhook_events`, migration 0011) and shown on Account → Server → Webhooks,
  which also makes the API keys and shows the URLs to paste. **Per-season requests**
  (migration 0012/0013): Discover's title page lists every TMDB season with In library /
  Requested / Request; the library's season dropdown includes seasons TMDB knows and the
  library lacks, with a Request row; Sonarr gets per-season monitoring + SeasonSearch;
  `requested_by` names the profile. Season rows unfold into TMDB's episode list with a tick
  per episode the library holds ("9 of 10 episodes", "Request rest"), and Sonarr's add
  ignores episodes that already have files. Verified end to end against the test library with
  replayed payloads (Download → Rename → EpisodeFileDelete → SeriesDelete).
- **Owner priorities, in their words (2026-10-14):** efficiency ("if it's not better than
  Jellyfin why use it"), **downloads** (and watch them when the server is unreachable),
  **profiles** per account with a profile picker at launch, a **Discover** tab that talks
  to the arr apps through TMDB ("our own request app"), Chromecast if feasible (would need the
  server to serve HLS; mpv playback does not cast), notifications. Dub | Sub on series
  slides needs the server to aggregate audio languages per series (today it is derived
  from the continue-watching episode's streams only).
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
