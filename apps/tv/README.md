# Roxy TV app

Android TV client for **Roxy Server**, written in Kotlin with Compose for TV
(`androidx.tv.material3`) and **libmpv + libass** for playback. It speaks Roxy's native
`/v1` API only (ADR-0007): no Jellyfin SDK, no Jellyfin login. The Jellyfin-compatible
surface on the server exists for request app and the arr apps, not for this app.

Look and feel follows the owner's brief: dark, Netflix-style. Home has a hero banner and
horizontal rows; cards scale on focus; detail pages have a backdrop hero with a season row
and an episode list. Everything is D-pad first but also works by touch, so the APK installs
on a phone for development (no `leanback` requirement).

## Layout

```
app/src/main/kotlin/tv/roxy/tv/
  RoxyApp.kt                 Application: fonts install, Prefs, RoxyApi, Coil ImageLoader (Bearer token)
  MainActivity.kt            Login vs. the TV or phone graph; portrait on phones, landscape on TV; token check via GET /v1/auth/me
  api/RoxyApi.kt             suspend client + wire types (Item, UserData, HomeResponse, MediaSource, ...)
  data/Prefs.kt              DataStore: Session (server, token, user), PlayerSettings, downloads Wi-Fi-only flag, cached profiles
  data/Updates.kt            update check against GET /v1/apps/tv/latest, APK download, package-installer hand-off
  data/downloads/Downloads.kt   download index (JSON) + files under Android/data/<app>/files/downloads/<itemId>/; offline progress queue
  data/downloads/DownloadService.kt  dataSync foreground service: one video at a time, HTTP-range resume, images + sidecar subs, notification
  diagnostics/DeviceReport.kt SoC / display / decoder report (logged at start, shown in Settings)
  player/MpvPlayer.kt        libmpv wrapper: fonts.conf + FONTCONFIG_FILE, track policy (from the spike)
  player/MpvStats.kt         stats overlay snapshot + CPU sampler
  player/PlayerActivity.kt   full-screen player, OSD, stats overlay, progress reporting
  ui/theme/Theme.kt          dark colour scheme
  ui/nav/Routes.kt           typed Navigation Compose routes (Home, SeriesLibrary, MovieLibrary, Settings, Series, Movie)
  ui/components/             MediaCard, MediaRow, RoxyImage (placeholder tile), buttons, text field (TV, tv-material)
  ui/phone/                  phone layout (Material 3): PhoneRoot bottom bar, PhoneHome hero carousel + rows,
                             PhoneBrowse / Watchlist / Search grids, PhoneSeries / PhoneMovie title pages, PhoneAccount
  ui/phone/PhoneDownloads.kt episode/season/movie download controls, the Downloads page (grouped by show, storage bar), downloaded-show page
  ui/profiles/ProfilePicker.kt "Who's watching?" picker + profile editor (AniList character avatars); both layouts
  ui/login/LoginScreen.kt    server URL / user / password -> POST /v1/auth/login
  ui/home/HomeScreen.kt      hero + rows from GET /v1/home (falls back to /v1/items on 404)
  ui/detail/SeriesScreen.kt  backdrop header, season row, episode list (GET /v1/items?parent=...)
  ui/detail/MovieScreen.kt   same header without seasons
  ui/settings/SettingsScreen.kt hwdec, vo, audio language order, subtitle mode, stats overlay, log out
  ui/Playback.kt             PlayRequest (local file or stream URL + start), playItem, nextEpisode (series order; downloads when offline)
  player/NextEpisodeCard.kt  Netflix end card: thumb, countdown, Play now / Watch credits
```

Architecture: no DI framework. `RoxyApp` owns `Prefs` and `RoxyApi`; screens get simple
`ViewModel`s with `StateFlow` UI state through `viewModel { ... }` initializers. Navigation
is Navigation Compose with `@Serializable` routes (`ui/nav/Routes.kt`); each back-stack
entry owns its ViewModels, and the phone's bottom-bar tabs save and restore their state.

**Two layouts, one app.** `MainActivity` picks at start-up: on Android TV (leanback /
`UI_MODE_TYPE_TELEVISION`) it locks to landscape and shows the D-pad screens in
`ui/home`, `ui/detail`, `ui/settings` (tv-material, 48 dp overscan padding, focus scale).
On a phone it locks to portrait, draws edge-to-edge and shows `ui/phone/*`: Netflix's
phone layout built on Material 3 for the theme, ripples, bottom bar, dialogs and switches,
with the look-defining pieces drawn by hand. Home = "Roxy" wordmark over a blurred
backdrop glow, a big rounded 2:3 hero card (poster, title, genres, white **Play** /
grey **Info**), then rows: 16:9 thumbs with a play badge and progress for Continue
Watching, plain 2:3 posters for the rest. Bottom bar: Home, Series, Movies (3-column
poster grids) and Settings. Title pages: 16:9 backdrop with a round back button, title,
meta line (year, seasons, rating), full-width Play / Resume, tap-to-expand synopsis,
season chips plus "Movies & OVAs", and Netflix-style episode rows (thumb, "N. Title",
runtime, synopsis). Both layouts share the ViewModels and the player.

Server contract: `docs/api/native.md`. The app is lenient (unknown JSON keys ignored) and
degrades when the server is behind: a 404 on `/v1/home` composes the rows from
`/v1/items`, missing `images` keys render a dark tile with the title, missing
`userData` just hides progress bars.

## Prerequisites

1. **Android SDK.** Android Studio's bundled JDK plus `platforms;android-37`,
   `build-tools;37.0.0`, `platform-tools`. The dev laptop has these installed headlessly
   under `%LOCALAPPDATA%\Android\Sdk`; `local.properties` points at it.
2. **Fonts.** Run `.\tools\fetch-fonts.ps1` once. It downloads the fallback bundle (~36 MB,
   50 files, git-ignored) into `app/src/main/assets/fonts/` and writes `FONTS.md` with
   hashes and licences: Open Sans (Crunchyroll's dialogue font, never attached), Liberation
   Sans/Serif (Arial/Times metric-compatible), Carlito (Calibri), Lato, Montserrat, Source
   Sans 3, Fira Sans, Cabin, Gandhi Sans, Noto Sans, DejaVu Sans, Noto Sans CJK JP,
   Noto Sans Symbols / Symbols 2 / Math / Music / Emoji, and Symbola 9.00 (the libass
   maintainers' suggested symbol net; later Symbola versions are personal-use only).
   `tools/font-coverage.py --fonts app/src/main/assets/fonts <subs...>` reports which
   bundled font covers which codepoints of a set of subtitle files. The app copies the
   fonts to its files dir and writes a fontconfig file with aliases (Arial -> Liberation
   Sans, Calibri -> Carlito, Myriad Pro -> Source Sans 3, ...) that libass reads through
   `FONTCONFIG_FILE`. Colour emoji fonts are rejected on purpose: libass only renders
   outline glyphs, so a monochrome Noto Emoji is what makes emoji show up at all.
3. **Device in developer mode.** Phone: Settings > About phone > Software information >
   tap Build number 7 times, then Developer options > USB debugging. TV: Settings >
   Device Preferences > About > tap Build 7 times, then Developer options > USB/Network
   debugging, and note the TV's IP. Samsung phones may also need Auto Blocker off to
   accept `adb install`.
4. **Optional dev defaults.** Copy `spike.properties.example` to `spike.properties` (git-
   ignored) to prefill the login screen of the *debug* build with your server URL, user and
   password. Nothing is baked into release builds; the token lives in DataStore after login.

## Build and run

```powershell
$env:JAVA_HOME = "C:\Program Files\Android\Android Studio\jbr"   # bundled JDK
$env:ANDROID_HOME = "$env:LOCALAPPDATA\Android\Sdk"
.\gradlew.bat assembleDebug --no-configuration-cache   # -> app\build\outputs\apk\debug\app-debug.apk
.\gradlew.bat --stop                                   # free the daemon's memory when done

adb devices                      # phone over USB, or: adb connect <tv-ip>:5555
adb install -r app\build\outputs\apk\debug\app-debug.apk
adb shell am start -n tv.roxy.tv.debug/tv.roxy.tv.MainActivity
```

`--no-configuration-cache` matters after editing `spike.properties`: the cached
configuration otherwise keeps the old defaults baked into `BuildConfig`.

The debug APK is ~110 MB (libmpv for arm64-v8a and armeabi-v7a plus the font bundle) and
installs on phones as well as TVs. Roxy Server must listen on the LAN
(`ROXY_LISTEN=0.0.0.0:8096`) and have media scanned.

## Screens

| Screen | Data | Notes |
|---|---|---|
| Login | `POST /v1/auth/login` | token + user persisted in DataStore; `GET /v1/auth/me` on launch, 401 logs out |
| Home | `GET /v1/home` | hero = first `continue` item, else first recent episode; rows continue / recentEpisodes / recentMovies / series / movies; falls back to `/v1/items` queries when the server has no `/v1/home` |
| Series | `/v1/items/{id}`, `?parent=<series>&type=season`, `?parent=<season>&type=episode&include=sources&sort=index` | season row, episode list with thumb, number, runtime, progress, stream summary |
| Movie | `/v1/items/{id}` | Play / Resume / Play from start |
| Player | `/v1/stream/<sourceId>?api_key=<token>` | start = `userData.positionMs`; `POST /v1/me/progress` every 10 s, on pause and on stop (with mpv's `ff-index` as audio/subtitle index) |
| Settings | DataStore | hwdec, vo, audio language order (English dub first by default), subtitle mode, stats overlay, log out |

Images go through Coil with the API's OkHttp client, so every request carries
`Authorization: Bearer`; URLs are `serverUrl + images[type] + ?w=<card px>`. Items without
an `images` entry never hit the network.

Player controls (`player/PlayerControls.kt`), laid out like YouTube: tap the video or
press any key to show them; they auto-hide after 4 s unless paused or a sheet is open.
Top: back + title. Centre: -10 s, play/pause, +10 s as plain translucent dark discs
(no outline; a remote's focus adds a white ring, touch shows none).
Bottom: red seek bar with a round thumb and the buffered range; while dragging or
stepping it shows the **trickplay preview** (server-generated thumbnail for that time).
Button row, left: play/pause, mute, `elapsed / total`; right: **CC** (toggles the last
subtitle track), **Settings** (a YouTube-style popup above the gear: quality, playback
speed, audio, subtitles, auto-skip; tapping anywhere else closes it), **picture-in-picture**
(phones only; also entered automatically when leaving the app while playing), **fill/fit**
(crop to the screen or letterbox). Autoplay of the next episode is always on, so there is
no switch for it. Top-right shows the clock and **"Ends at …"** (remaining time at the
current speed, in the device's time zone). **Double-tap** the left or right half of the
video to seek ±10 s with a flash saying so, like YouTube. **Pinch to zoom** (phones): two
fingers zoom about the point between them, up to 8×, in 0.1× steps with a "1.5×" readout
at the top; the level stays after letting go, one finger pans while zoomed, and letting
go at or near 1× eases back to normal. The player is updated once per frame (mpv
`video-zoom` / `video-pan`), never per touch event. The seek bar is drawn by hand (full
width, red from 0:00, round thumb; intro/outro ranges show as lighter ticks).

**Skip intro / outro.** The server derives skippable segments from chapter markers
(`segments` on the media source: an OP-length chapter near the start, an ED-length one
near the end, plus a short preview after the ED). While the playhead is inside one, a
"Skip intro" / "Skip outro" pill sits bottom-right over the video, visible even with the
controls hidden. **Auto-skip intro & outro** in the Settings popup jumps over them
automatically (persisted; seeking back into a segment re-arms it). Files without chapters
get no segments yet; audio fingerprinting across a season is the follow-up.

| Key | Action |
|---|---|
| Any D-pad key (controls hidden) | show controls; Left/Right also seek ±10 s immediately |
| Play-Pause / Space | toggle pause |
| Rewind / Fast-forward | seek -10 s / +10 s |
| Captions / A | open the Subtitles / Audio sheet |
| Menu / I | toggle the diagnostics stats overlay |
| Back | close sheet → hide controls → exit (reports the final position) |

Track policy: audio by the Settings language order (`eng` then `jpn` by default);
subtitles by server-detected language (`detectedLanguage` beats the container tag), then
title class (full dialogue vs signs/songs per the Settings subtitle mode), then ASS over
SRT. The file's default flag is only a tie-breaker, because dual-audio releases flag the
signs track.

## Diagnostics

`PlayerActivity` is exported in the debug build so adb can launch it with extras. Every
extra is optional and falls back to the saved Settings: `url`, `title`, `hwdec`, `vo`,
`start` (seconds), `submode` (full | signs | none), `sid` (force an mpv subtitle id),
`sourceId` (lets the player ask Roxy Server for sniffed subtitle languages; a
`/stream/<uuid>` URL implies it), `itemId` (enables progress reporting), `alang`
(comma-separated audio language order), `stats` (boolean, show the overlay from the start).

```powershell
adb shell am start -n tv.roxy.tv.debug/tv.roxy.tv.player.PlayerActivity --es url "http://<server>:8096/stream/test-hi10p-ass" --es hwdec no --es vo gpu --ei start 0 --es submode full --ez stats true
```

### Scripted test matrix

Runs every test file through every `hwdec` mode, screenshots the stats overlay, saves the
mpv log, and summarises decoder / subtitle / font selection in `results.csv`:

```powershell
.\tools\spike-matrix.ps1                                   # 10 files x 3 hwdec, vo=gpu
.\tools\spike-matrix.ps1 -Vos gpu,gpu-next -Files test-hi10p-ass,test-hevc10-hdr10-4k-ass
```

Output lands in `spike-runs/<timestamp>/` (git-ignored). Dropped frames and CPU are on
the overlay in each PNG. Write the conclusions to `docs/research/player-spike-<model>.md`.

Symbol / emoji / CJK fallback (plays `test-symbols-fallback`, which attaches no fonts, and
prints which bundled font every missing glyph resolved to):

```powershell
.\tools\spike-symbols.ps1
```

Seeking and real files (start past the intro, pick the full dialogue track):

```powershell
.\tools\spike-seek.ps1 -File <source id> -Hwdec mediacodec -StartSeconds 360 -SubMode full
```

### What each test file proves

| File | What it proves |
|---|---|
| `test-hi10p-ass` (H.264 High 10, heavy typesetting) | software decode + libass cost on this SoC (no SoC decodes Hi10P in hardware) |
| `test-hevc10-ass` (HEVC Main10, karaoke) | hwdec path + libass timing |
| `test-hevc10-hdr10-4k-ass` | hwdec at 4K, HDR tone mapping, libass on a 4K canvas |
| `test-dualaudio-ass` (FLAC jpn + E-AC-3 eng, ASS + SRT, sidecars) | audio/sub track selection |
| `test-av1-10bit-ass` | AV1 hwdec availability |
| `test-h264-8bit-ass` | the common case; 8-bit H.264 hwdec baseline |
| `test-hevc8-60fps-51-ass` (FLAC 5.1 + E-AC-3 5.1) | 60 fps hwdec, multichannel audio / passthrough |
| `test-bluray-pgs` (TrueHD 5.1 + AC-3 5.1, PGS) | bitmap subtitle rendering, lossless audio |
| `test-hevc10-hlg-ass` | HLG signalling and tone mapping |
| `test-h264-mp4-movtext` | MP4 container, mov_text subs |

Read from the overlay: `hwdec=` (actual decoder), `drops:` (vo / decoder / delayed /
mistimed), `fps` output vs container, `cpu`, and whether the font log shows
`fontselect ... -> <attachment name>` for the MKV-attached fonts.

Decisions the matrix feeds: default engine for the TV (libmpv vs Media3 + ass-media),
default `hwdec` and `vo` for the TV's SoC, and whether 4K ASS needs the server to burn in.
The phone run (Galaxy S25 Ultra, `docs/research/player-spike-SM-S938U.md`) validates
correctness only; the performance decision needs the Costco TV.
