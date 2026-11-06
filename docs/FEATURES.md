# What Roxy does

The full feature list, server and apps. The short version is in the [README](../README.md).

## Roxy Server

(`server/`, Go 1.27, SQLite, jellyfin-ffmpeg shelled out)

- Library scan of TRaSH-style folders (series/year/tvdb tag, SxxExx and absolute numbers,
  specials, sidecar subtitles, `S02E01v2`-style revisions, "<folder name> NN" files such as
  fan-edit arcs), ffprobe of every file, eager font and attachment extraction,
  range-streamed direct play, subtitle serving with language sniffing for mislabelled tracks.
- Metadata from TMDB and AniList with a local image cache and on-the-fly resizing; AniList
  sequel chains become seasons; missing and unaired episodes show as placeholders (an
  announced season or an episode with no air date yet reads "Not out yet"); age ratings
  are shown as issued (TV-Y7, TV-14, PG-13), with content reasons from TMDB or AniList
  tags; an art
  picker lets an admin choose any TMDB or fanart.tv banner, poster or logo, and the pick is
  locked against refreshes.
- Intro and outro markers from chapter markers, else by audio fingerprint across a season
  (Chromaprint, Intro Skipper's method); seek-preview thumbnails (trickplay).
- First-time setup like Jellyfin's: a new server has no accounts, and the first person to
  open the app or the web page on the home network creates the admin (account, TMDB key,
  media folders). After that the setup door is shut.
- Users with device tokens, profiles with AniList character avatars, per-profile watch
  state, favourites, Continue Watching, a Home feed. Device Management lists every device
  signed in with the login (online or not, what it is playing, home network / Tailscale /
  internet, the address only behind Show) and can sign any of them out.
- Discover: trending, seasonal anime, search, filters, title pages with per-season and
  per-episode requests sent to Sonarr and Radarr, with live download status (progress,
  time left, stalled, failed with the reason, nothing found yet) and Search again / Try
  another release; until Sonarr and Radarr are connected the app offers to set them up.
  Sonarr and Radarr webhooks keep the library in step without a rescan.
- Transcoding for lower qualities: H.264 in Matroska with every subtitle track, font and
  chapter kept, audio chosen per track (5.1 kept where it can be), on NVIDIA NVENC, Intel
  Quick Sync, VAAPI, AMD AMF or the CPU. Auto picks the best one; an admin can choose in
  the app. Runs in Docker with the GPU passed through.
- LAN discovery (a UDP beacon on 8096), in-app APK updates, server-served Terms of Use and
  Privacy. (The Jellyfin-compatible API was removed; see ADR-0008.)
- Requests go straight to Sonarr and Radarr. They are offered only once Sonarr
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
  - With Prowlarr configured (`ROXY_PROWLARR_URL`, `ROXY_PROWLARR_API_KEY`), Roxy finds
    the season releases whose names Sonarr cannot read (such as "(Season 1)"): it rewrites
    the name, has Sonarr score it, and pushes it only when it beats both Sonarr's best pack
    and its episode-by-episode result. Twice a day it looks again at finished seasons whose
    grabs were all sub-only, because dubs often land weeks after the subs and Sonarr never
    goes back to look. Sonarr still decides every push.
  - Status reads what is happening: a just-added series is "loading the episode list",
    a release grabbed a moment ago is queued, a complete season with one upgrade in
    flight is complete ("Upgrading S1 E2 to a better release").
- Saving settings from an older app never wipes a setting it does not know about.
- Security-audited (2026-11-03): MKV attachments served only as fonts, symlinks out of the
  media folders refused at scan and serve, API keys good only for the webhook and ingest
  endpoints, `X-Forwarded-For` believed only from `ROXY_TRUSTED_PROXIES`, capped request
  bodies and subtitle extraction, ffmpeg inputs limited to local files, a strict CSP on the
  web app, device tokens that expire after 180 days unused, an account-wide slowdown on
  wrong passwords that never locks the owner out.
- Sign-in by phone: a TV or browser shows a QR code and a six-letter code, and a signed-in
  phone approves it. The sign-in page's poster wall comes from the server's own library
  (`/v1/showcase`, public, posters only).
- Profile avatars: "From your library" characters are saved per series by a slow background
  worker (AniList for anime; TMDB's cast for live action; none for other animation), so the
  editor opens instantly and a thousand new shows are looked up over hours, not at once.

## Roxy Android app

(`apps/android/`, Kotlin, Jetpack Compose, libmpv; minSdk 28; version 0.5.27)

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
- The login screen lists servers found on the network; tap one to fill the address. A
  found server must advertise its own or a private / tailnet address, and the app pins the
  server it signed in to.
- Setup wizard on the phone and a separate one on the TV built for the remote.
- Survives server restarts: downloads wait and resume by range, the player pauses at the
  position and carries on when the server is back, progress is queued and sent later.
- Updates install only when the APK is this app, signed with the same certificate, newer,
  and matches the sha256 the server published.
- Browse on the phone: categories (Newly Added, A-Z, Series, Movies, genres) as chips over a
  poster grid with the title and year under each poster.

Phone screens are verified on a Galaxy S25 Ultra after every change; the TV layout runs on
a TCL Q77K (Google TV).

## Roxy Web

(`apps/web/`, TypeScript, React, Vite; started 2026-10-26)

- The computer client: Crunchyroll's layout at desktop width (`docs/design/roxy-look.md`),
  the same `/v1` API as the Android app, served by the server itself at `/`.
- First-time setup in the browser (the same steps as the phone), Device Management, card
  menus and hover panels with the synopsis on every row, rows that drag sideways.
- Sign-in over a poster wall (password, or approve from a phone), "Who's watching?" as on
  the apps, Browse with a category list and genres, Discover with title pages, request
  status and download progress, and the Sonarr / Radarr settings for admins.
- Playback is the browser's own decoder with no transcoding: MKV, H.264 (8-bit and
  Hi10P), HEVC 10-bit (GPU), AV1; ASS/SSA rendered by jassub (libass in WebAssembly)
  with the fonts from the MKV. Verified in Chrome on every file in the test library,
  including the KonoSuba opening and ending karaoke. Chrome and Edge are the targets;
  Firefox cannot play MKV or Hi10P.

