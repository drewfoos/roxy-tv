# Alternative Self-Hosted Media Servers to Jellyfin for an Anime-Heavy Library (ASS subtitles, request app, *arr stack) — as of 2026-10-13

Scope note: Research covered Plex, Emby, Jellyfin (official + third-party clients/forks), Seanime, Kyoo, Shoko/Shokofin, Stremio, Dim, Olaris, Wizarr, SuggestArr, and the request app/servarr integration layer. Primary sources were preferred (project repos, official docs, vendor forums with staff replies). Several official pages returned HTTP 403 to the fetcher (plex.tv blog, Plex support article on hardware transcoding, servarr wiki body); where that happened I relied on secondary coverage and flagged it.

---

## Key Question 1: Which servers does request app officially support for auth and library sync as of late 2026?

### Takeaway
request app (the merged request app + request app project) officially supports Plex, Jellyfin, and Emby — simultaneously, in any combination — for both user authentication/import and library scanning, and its request backends remain Sonarr and Radarr only. The latest release at research time is v3.5.0 (2026-09-28).

### Cited Findings
- request app README: it "integrates with the media server of your choice: Jellyfin, Plex, and Emby", with "Full Jellyfin/Emby/Plex integration including authentication with user import & management" and a "Jellyfin/Emby/Plex library scan, to keep track of the titles which are already available." License is MIT. — [request app-team/request app README]((link removed))
- Request backends: "Currently, request app supports Sonarr and Radarr. More to come!" — [request app-team/request app README]((link removed))
- A fork README states request app supports Jellyfin, Plex, and Emby simultaneously so you can configure any subset and allow users from any enabled provider to sign in. — [regix1/request app fork README (mirror of upstream text)]((link removed))
- The README has a "Migrating from request app/request app to request app" section pointing to a release announcement and migration guide. — [request app-team/request app README]((link removed))
- The request app release post (republished on Lemmy/PieFed) says the request app and request app developers now work together as one group called request app, giving a single shared codebase that carries over request app's features, the newer request app features, and support for Jellyfin and Emby. — [request app & request app to merge into request app (Lemmy repost)](https://scribe.disroot.org/comment/9449842)
- Latest release: tag v3.5.0, published 2026-09-28T00:13:38Z. Notes include: a breaking API change where GET library endpoints for Plex and Jellyfin no longer accept `sync`/`enable`; library sync now uses `POST /settings/{plex|jellyfin}/library/sync` and enable/disable uses `PUT /settings/{plex|jellyfin}/library/{libraryId}`; Jellyfin Authorization headers updated (#3502); Emby servers now hide the Quick Connect button on login (#3369); connection failures no longer masked during media-server sync and login (#3324); support for a custom Plex metadata provider GUID scheme in the scanner (#3316). — [GitHub API: request app releases/latest]((link removed))
- docs.request app.dev/getting-started lists install methods (Docker recommended, source, Kubernetes, Nix) and the web UI at port 5055; it does not itself list servers. — [docs.request app.dev Getting Started]((link removed))
- Adjacent tools: Wizarr (invite/user management) supports Plex, Jellyfin, Emby, Audiobookshelf, Romm, Komga, Kavita; SuggestArr reads watch history from Jellyfin, Plex, and Emby and sends requests to request app/request app/request app (older pages list only Jellyfin + Plex; Emby appears newer). — [wiki.serversatho.me/wizarr](https://wiki.serversatho.me/wizarr); [SuggestArr docs (ElfHosted)](https://docs.elfhosted.com/app/suggestarr/); [SuggestArr source summary](https://www.sourcepulse.org/projects/25022344)

### Inferences
- Because request app supports all three servers at parity for auth + library sync, choosing between Plex/Emby/Jellyfin does not affect request-management capability; the decision can be made purely on playback/subtitle/pricing grounds.
- The 3.5.0 API path naming (`/settings/{plex|jellyfin}/...`) suggests Emby shares the Jellyfin code path internally (the Emby-specific change in 3.5.0 is only a UI tweak), consistent with Emby support being derived from the Jellyfin integration.

### Gaps
- Could not fetch the docs.request app.dev "Using request app" settings pages (only the install landing page loaded), so exact wording of per-server auth options (e.g., Plex OAuth vs. Jellyfin/Emby local login, Quick Connect) is from the release notes rather than the docs.
- Did not find any request app support for servers other than the big three (no Kyoo/Seanime support found; "More to come" is unspecified).

---

## Key Question 2: Does Plex or Emby render ASS natively in their Android TV apps, or burn in? Any 2025–2026 changes?

### Takeaway
Neither Plex nor Emby reliably renders styled ASS natively on Android TV as of Oct 2026. Emby's Android/Android TV apps embed a libass JNI renderer but it fails on many real-world anime ASS tracks (staff acknowledged an outdated libass, no ETA); Plex removed libmpv from its mobile/Apple TV apps, and users report ASS is "completely broken" there as of Sept–Oct 2026, with "burn subtitles: always" (server transcode) as the practical workaround.

### Cited Findings
**Emby**
- Feb 6, 2025 report: internal ASS subtitles do not display on Emby for Android TV with Direct Play; SRT works; same file renders on Web 4.8.11.0, iOS 2.2.37, Chromecast 2.1.0, Windows 3.0.20, Mac 2.2.33. Logs show repeated `libass_jni: Empty event`. Emby staff (ebr): "The internal renderer doesn't like these subs at all"; Luke: "We can look at updating the libass version it is using" (Feb 23, no ETA). Still reproducible on Android app 3.4.56 (Apr 15, 2025); transcoding (burn-in) works. Luke noted VLC/MXPlayer (which play the file) do not use ExoPlayer. No resolution on page 1. — [Emby community topic 136069](https://emby.media/community/topic/136069-ass-subtitles-dont-render-on-android-tv)
- March 2025 Emby community comments: ASS handling is done by the app rather than the TV; heavy animation can exceed what TV hardware can handle; some ASS commands are unsupported; disabling text shadow in the ASS file was suggested as a workaround. — [Emby topic 136822](https://emby.media/community/topic/136822-ass-subtitle-missing-part)
- Older context (2020–2021): Emby's Android mobile app used a modified ExoPlayer that could render ASS, Android TV "would come later", and karaoke/effects still failed in 2021. — [Emby topic 91995 (Shield TV and ASS)](https://emby.media/community/topic/91995-shield-tv-and-ass-subtitles)
- Emby's subtitle support article lists ass, srt, ssa, sub/idx, vtt as supported formats and covers naming/flags only; it does not document burn-in vs. text delivery or font handling. — [emby.media Subtitles article](https://emby.media/support/articles/Subtitles.html)

**Plex**
- Plex forum thread (opened Sept 23, 2026): the mobile apps dropped libmpv "last year" (i.e., 2025), which "ruined" .ASS playback; the newer update also came to Apple TV. Symptoms: fonts don't load; anything beyond basic bottom-of-screen text stutters. Oct 13, 2026 post: simple ASS works on the recent Apple TV update but advanced placement/movement "slows rendering to a crawl"; "anime is unwatchable". Workarounds: set "burn subtitles" to Always (ends direct play), use Plezy (third-party, mpv-based) or Infuse, or revert Apple TV app to 8.45. A user (Atomatth, Sept 24) linked an existing open internal issue and requested logs; no fix or timeline. The thread names iOS and Apple TV; Android TV is not mentioned. — [Plex forum: Plans to bring back subtitle support?](https://forums.plex.tv/t/plans-to-bring-back-subtitle-support/943302)
- Older Plex forum post: Plex on Android phones plays ASS without transcoding, but Android TV and Plex Web transcode. — [Plex forum: Plex ASS subtitle support](https://forums.plex.tv/t/plex-ass-subtitle-support/299268)
- Plex forum (Shield/Android): "the Android and Shield apps are currently not up to the task of correctly rendering complex ASS subtitles common in fansubbed anime"; server burn-in fallback quality "suffers badly". — [Plex forum: Nvidia SHIELD/Android app SSA/ASS](https://forums.plex.tv/t/nvidia-shield-android-app-ssa-ass-format-anime-subtitles-transcoding-problems/446811.md)
- Plex forum: Plex Media Server can transcode ASS to SRT (styling lost) or, with client image-subtitle setting on auto, burn styling in. — [Plex forum: implemented ASS/SSA support](https://forums.plex.tv/t/ass-ssa-subtitle-support/543947)
- A July 2026 Plex forum listing includes an open thread "External ASS subtitle transitions render incorrectly during burn-in, while embedded ASS works". — [Plex forum subtitle tag listing](https://forums.plex.tv/tag/subtitles/122)
- APKMirror changelogs for Plex Android 6.9.0/6.10.0 list "local playback: text based ASS/SSA subtitle support" (dating unclear). — [APKMirror Plex 6.10.0.2916](https://www.apkmirror.com/apk/plex-inc/plex/plex-6-10-0-2916-release/)

### Inferences
- Emby is technically "native ASS" on Android/Android TV (it ships a libass JNI renderer) but the libass build is old enough that modern typesetting fails; practical outcome for an anime library is frequent silent subtitle loss unless you force transcode/burn-in.
- Plex's 2025 removal of libmpv moved it backwards for anime; the fact that the Apple TV app regressed in Sept 2026 suggests Plex is standardizing on a non-mpv player stack across platforms, so Android TV is unlikely to gain full libass rendering soon. No Plex staff statement either way was found.
- For both, "burn-in on server" means a full video transcode (needs Plex Pass / Emby Premiere for hardware acceleration), which conflicts with the goal of direct-playing 10-bit HEVC.

### Gaps
- No Plex official release note or support article in 2025–2026 describing ASS handling in the Android TV app was found; the plex.tv blog and support pages returned HTTP 403 to the fetcher.
- No Emby Android TV changelog entry confirming a libass version bump after Feb 2025 was found (page 2 of the Emby thread was not retrieved).
- Embedded-font (attachment) support on Plex/Emby Android TV was not confirmed by any source.

---

## Key Question 3: Are there anime-specific servers/clients (Seanime, Shoko + Shokofin, anime-focused Jellyfin forks) that do styled subs well and still work with the arr stack?

### Takeaway
Yes, but the strongest "styled subs done right on a TV" options are Jellyfin-side clients/forks (Jellyfin Thor fork with libass 0.17.5; official Jellyfin Android TV 0.20.0 beta adding ASS direct play; mpv-based Streamyfin/Wholphin; libVLC) rather than separate servers; Seanime (Go, GPL-3) and Kyoo (GPL-3, single developer) both support ASS/SSA with embedded fonts but have no Sonarr/Radarr/request app integration and no Android TV client. Shoko + Shokofin remains active (last push Apr 2026) and is a metadata layer on top of Jellyfin, so it keeps full arr/request app compatibility.

### Cited Findings
**Jellyfin official Android TV**
- Jellyfin Android issue #1833 (opened Oct 31, 2025): maintainer nielsvanvelzen (Jellyfin member) wrote on May 5, 2026 that ASS support is "already developed and going to be released as experimental option in the next version of the TV app," with a mobile port "later". Approaches discussed: libass directly, mpv (libmpv-android) as a Media3 Player; a community PR #5577 to jellyfin-androidtv was rejected as AI-generated. — [jellyfin/jellyfin-android#1833](https://github.com/jellyfin/jellyfin-android/issues/1833)
- jellyfin-androidtv releases: v0.20.0-beta.1 (08 Sep): "Support Advanced SubStation Alpha direct play", "Add option to always burn in subtitles when transcoding", "Add user-defined AVC/HEVC codec level override", "Refactor: Extract HEVC and AV1 codec modules", "Allow selecting external video player app". v0.20.0-beta.2 (14 Sep): "Update androidx.media3 to v1.11.1". v0.20.0-beta.3 (24 Sep): "Increase maximum subtitle size to 48". v0.19.10 (16 Aug) is marked Latest stable. v0.19.5 (13 Dec): "Add AV1 detection support for older Android versions". Years are not shown on the page. — [jellyfin-androidtv releases](https://github.com/jellyfin/jellyfin-androidtv/releases)
- Earlier status (Nov 30, 2024): "Not any time soon" — "there is no Android compatible build of libass... exoplayer/media3 doesn't support rendering .ass currently." — [Jellyfin forum: will the Android TV app ever support ASS](https://forum.jellyfin.org/t-will-the-jellyfin-android-tv-app-ever-support-ass-subtitles)
- Moderator workaround (2024–2025): in Android TV client settings choose the "LibVLC" player, which handles ASS; ExoPlayer has basic ASS support only. — [Jellyfin forum: ASS subtitles not supported](https://forum.jellyfin.org/t-ass-subtitles-not-supported?pid=18157)
- Oct 2025 user on Android TV 0.19.0 still reported ASS and PGS problems. — [Jellyfin forum thread 14351](https://forum.jellyfin.org/printthread.php?tid=14351)

**Jellyfin Android TV forks / third-party clients**
- Jellyfin Thor (thor2002ro/jellyfin-androidtv): unofficial fork, GPL-2.0, package `org.jellyfin.androidtv.thor` (installs side-by-side), sideload APK, Android TV 7.0+. Adds custom libass build (libass 0.17.5 as libass-android 0.5.1-thor) with a custom Media3 renderer, renderer prewarming, HDR-specific text colors, 24–60 FPS subtitle limit; Media3/ExoPlayer 1.11.1 with Media3 FFmpeg decoder (FFmpeg 8.1.2) and HW/SW/FFmpeg decoder selection with fallback; MPV (mpv-android-lib 0.2.2-thor) option; Dolby Vision via libdovi-android; DD+ Atmos/TrueHD Atmos/DTS:X/DTS-HD badges; Shield fallbacks for some H.264 Hi10P streams. "Not supported by the Jellyfin team". — [thor2002ro/jellyfin-androidtv](https://github.com/thor2002ro/jellyfin-androidtv)
- Swiftly Player comparison (dated 17 Sept 2026) of Android TV Jellyfin clients: official app (Media3 ExoPlayer + Jellyfin FFmpeg decoder, article says libass for ASS/SSA); Wholphin (ExoPlayer or MPV; MPV direct-plays SSA/ASS, ExoPlayer has optional software SSA/ASS and AV1 decoding; open source); Moonfin (Media3; Dolby Vision P7 direct play + audio passthrough); Streamyfin (MPV; Android TV build is a GitHub APK, no dedicated TV UI); Kodi + Jellyfin for Kodi / JellyCon; Findroid (Android TV only planned); Swiftly Player (libVLC, MPL-2.0, Android TV in closed test). — [swiftlyplayer.com Jellyfin Android TV clients compared](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)
- JellyWatch Player advertises libass for "pixel-perfect ASS/SSA subtitles" on Android TV (commercial, competitor blog). — [JellyWatch Player](https://jellywatch.app/player); [JellyWatch TV player repo](https://github.com/JellyWatchteam/JellyWatch-Jellyfin-TV-Player)
- Litefin: Jellyfin client for Tizen, webOS, Android, Windows, Linux, macOS and Web. — [MoazSalem/litefin](https://github.com/MoazSalem/litefin)
- Plex third-party: Plezy uses mpv and "plays subtitles correctly" per Plex forum users (Apple platforms). — [Plex forum thread 943302](https://forums.plex.tv/t/plans-to-bring-back-subtitle-support/943302)

**Shoko / Shokofin**
- Shokofin "brings Shoko to Jellyfin", written in C#; last push Apr 11, 2026; 266 stars, 21 forks, 4 open issues. — [gittrend.io ShokoAnime/Shokofin](https://gittrend.io/repo/ShokoAnime/Shokofin)
- Shokofin provides AniDB-accurate anime metadata, automatic episode grouping, watch-tracking sync and anime ordering (secondary source). — [JellyWatch Shoko guide index](https://jellywatch.app/blog?q=shoko)

**Seanime**
- Open-source media server with web UI + desktop app for anime and manga; server in Go; frontend React; desktop "Seanime Denshi" (Electron) includes a libmpv-based player with shader support; "Seamless integration with MPV, VLC, and MPC-HC on desktop"; mobile opens files in Outplayer/VLC via intents; library streaming "on-the-fly transcoding or direct play to any device's web browser"; SSA/ASS explicitly supported; torrent streaming (BitTorrent, Torbox, Real-Debrid, AllDebrid, Premiumize); downloads via download client/Transmission; AniList integration; companion/mobile apps "Seanime Tenji" for iOS/Android; GPL-3.0; Android TV and Sonarr/arr integration not mentioned. — [5rahim/seanime](https://github.com/5rahim/seanime)
- A search for Seanime + Sonarr integration found no connection; adjacent tools found instead: Transpondarr (beta anime PVR using AniList + download client + Prowlarr/Torznab, hardlinks into Plex/Jellyfin), ani2arr (Firefox extension adding Sonarr/Radarr/request app actions to AniList pages), Moeradar (MAL/AniList to Sonarr/Radarr). — [pkg.go.dev transpondarr](https://pkg.go.dev/github.com/matthewdias/transpondarr); [ani2arr](https://addons.mozilla.org/it/firefox/addon/ani2arr/); [NCRoxas/moeradar](https://github.com/NCRoxas/moeradar)

**Kyoo**
- "A portable and vast media library solution" for movies, series, and anime; topics python/golang/react-native/next; dynamic transcoding with ABR plus original-quality playback; hardware transcoding referenced in install guide; "PGS/VOBSUB and SSA/ASS are supported, using embedded fonts when available, including on web and Chromecast"; anime explicitly targeted (unusual filenames should scan); intro/credit detection; OIDC login; Helm chart; clients: web, Android (VLC-based), Chromecast; Android TV "planned, no firm date"; GPL-3.0; 2.5k stars, 79 forks, 52 open issues; "developed by one person"; v5 rewrote most code; SIMKL sync and offline downloads "coming later". No Sonarr/Radarr/request app mention. — [zoriya/Kyoo](https://github.com/zoriya/Kyoo)
- Docker Hub backend image was last updated ~12 months before the indexed snapshot. — [hub.docker.com zoriya/kyoo_back](https://hub.docker.com/r/zoriya/kyoo_back)

### Inferences
- For "styled subs + arr stack + request app", the lowest-friction path is Jellyfin server (unchanged arr/request app integration) with an ASS-capable TV client: Jellyfin Thor (libass 0.17.5, most complete), official 0.20.0 beta (ASS direct play, experimental), or an mpv/libVLC-based client.
- Seanime and Kyoo are viable for a web/desktop/phone-centric household, but both lose on TV clients (no Android TV) and both would require a parallel request pipeline since request app does not know them. Seanime is better thought of as an AniList-driven downloader + desktop player than as a server replacement.
- Shoko/Shokofin does not change subtitle behavior (it's metadata); it is compatible with any of the clients above.

### Gaps
- Could not confirm whether jellyfin-androidtv v0.20.0 stable has shipped as of Oct 13, 2026 (page shows v0.19.10 as "Latest" and betas through 24 Sep); whether the beta's "ASS direct play" uses libass (vs. Media3's basic SSA decoder) is not stated in the release notes, and the issue thread says "experimental".
- Jellyfin Thor latest version number/date not shown on the repo page retrieved.
- No independent test of Streamyfin/Wholphin ASS rendering on Android TV was found (only engine descriptions).
- Seanime latest version/date not retrievable from the repo page.
- Tsukimi and Reiverr: searches returned nothing substantive; no status found. (Tsukimi is a GTK4/Rust desktop Jellyfin client and Reiverr a Jellyfin/Sonarr/Radarr web UI, per prior knowledge — unverified here.)

---

## Key Question 4: What does Plex's 2025 pricing change mean for remote streaming and mobile apps?

### Takeaway
Since April 29, 2025 remote streaming from a Plex server is no longer free: either the server owner holds Plex Pass (now $6.99/mo, $69.99/yr, $249.99 lifetime) or each remote user buys a Remote Watch Pass ($1.99/mo or $19.99/yr introductory, rising to $2.99/mo or $29.99/yr after June 1, 2026); in exchange the one-time mobile unlock fee was removed for the new apps, and a further lifetime price rise was signaled for July 1, 2026.

### Cited Findings
- Announced March 19, 2025, effective April 29, 2025: Plex Pass monthly $4.99 → $6.99, annual $39.99 → $69.99, lifetime $119.99 → $249.99; existing lifetime holders unaffected; lifetime purchasable at $119.99 until the deadline. — [9to5Mac](https://9to5mac.com/2025/03/19/plex-price-increase-remote-streaming-changes/); [BetaNews](https://betanews.com/2025/03/19/plex-2025-price-changes-remote-streaming-new-subscription/); [Plex blog: Important 2025 Plex updates](https://www.plex.tv/blog/important-2025-plex-updates/) (blog returned 403 to fetcher; cited via coverage)
- Remote streaming now requires Plex Pass (server side) or the new Remote Watch Pass ($1.99/mo or $19.99/yr at launch) for users streaming from someone else's server; the one-time mobile unlock fee was removed and local-network playback is free in the new mobile apps. Plex said the money funds a Common Sense Media integration, a new server management app, and an open API. — [9to5Mac](https://9to5mac.com/2025/03/19/plex-price-increase-remote-streaming-changes/); [Android Authority](https://www.androidauthority.com/plex-price-hike-3536765/)
- plex.tv/plans (Oct 2026): Remote Watch Pass introductory $1.99/mo until June 1, 2026 then $2.99/mo; $19.99/yr until June 1, 2026 then $29.99/yr (locked for 12 months if started in intro period). Plex Pass includes remote streaming for all users on your server, hardware transcoding, downloads, Skip Intro/Credits. Older mobile apps keep a one-minute playback limit removable by Plex Pass or one-time in-app unlock; Remote Watch Pass works only with the new apps. Page banner: lock in current lifetime price before July 1, 2026 (Plex Pass price figures failed to load on the page). — [plex.tv/plans](https://www.plex.tv/plans/)
- A third-party transcoding calculator site referenced a Plex price change dated July 2026 (unverified). — [minipclab transcode calculator](https://minipclab.com/tools/plex-transcode-calculator)
- Emby Premiere for comparison: $4.99/mo, $54/yr, $119 lifetime (single household, 30-device limit); gates hardware transcoding, "Free Apps" full access on Android/Fire TV/iOS/Xbox/PS4 (otherwise per-app unlock), offline downloads, DVR, Cinema Intros, CarPlay/Android Auto, scheduled conversion, backup, folder sync, smart-home voice. — [emby.media/premiere](https://emby.media/premiere.html)
- Secondary sources (June 2026) report Emby Premiere unchanged at $4.99/$54/$119 with a once-a-year Black Friday lifetime discount (~$99 in Nov 2025). — [JellyWatch Emby pricing 2026](https://jellywatch.app/blog/emby-premiere-pricing-cost-2026); [smarttvs.org Emby vs Plex](https://smarttvs.org/emby-vs-plex/); [mydealz Emby Premiere lifetime deal](https://www.mydealz.de/deals/emby-premiere-lifetime-fur-99-2685071)
- Jellyfin is GPL-2.0 licensed with ~56k GitHub stars (Aug 2026 snapshot); it forked from Emby in 2018. — [ideaproof.io Jellyfin](https://ideaproof.io/open-source/project/jellyfin); [learnwithhasan Jellyfin](https://learnwithhasan.com/self-hosted-apps/jellyfin/)

### Inferences
- For an anime household that watches away from home, Plex now costs at minimum $69.99/yr server-side (or $29.99/yr per remote viewer after June 2026) before any hardware-transcoding need; Emby lifetime ($119) is cheaper than Plex lifetime ($249.99) and Jellyfin is free. Because ASS burn-in on Plex/Emby forces transcodes, the hardware-transcoding paywall is practically mandatory for anime on those two.
- The "lock in lifetime before July 1, 2026" banner implies a second lifetime price increase occurred or was scheduled mid-2026; exact new figure not confirmed.

### Gaps
- Could not retrieve current (Oct 2026) Plex Pass monthly/annual/lifetime figures from plex.tv (pricing widget failed to load; blog 403). Figures above are the April 2025 ones plus the June/July 2026 scheduled changes shown on the plans page.
- Plex's open API / server management app delivery status was not researched.

---

## Key Question 5: Which server has the best Android TV experience for anime specifically (direct play of 10-bit HEVC + ASS)?

### Takeaway
Jellyfin, paired with a libass/mpv-capable Android TV client (Jellyfin Thor fork today; official 0.20.0 with ASS direct play once stable), is the only stack found that direct-plays 10-bit HEVC and renders styled ASS on Android TV without a server transcode; Plex and Emby both fall back to burn-in transcodes for complex ASS on Android TV.

### Cited Findings
- Jellyfin Thor: libass 0.17.5 renderer + Media3 FFmpeg software decoder (FFmpeg 8.1.2) with HW/SW fallback, Dolby Vision, Atmos/DTS:X badges, Shield Hi10P fallbacks. — [thor2002ro/jellyfin-androidtv](https://github.com/thor2002ro/jellyfin-androidtv)
- Official Jellyfin Android TV 0.20.0-beta.1: ASS direct play, HEVC/AV1 codec modules refactor, AVC/HEVC level override, external player selection. — [jellyfin-androidtv releases](https://github.com/jellyfin/jellyfin-androidtv/releases)
- Official Jellyfin Android TV uses Media3 ExoPlayer with Jellyfin's FFmpeg decoder; Swiftly Player (libVLC) direct-plays HEVC and AV1 on device; Wholphin MPV direct-plays SSA/ASS. — [swiftlyplayer.com comparison, 17 Sep 2026](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)
- Jellyfin server-side: Intel guide covers QSV (Broadwell+) and VA-API; AV1 decode from Gen 12-class graphics and AV1 encode from Intel Arc A-series / Core Ultra (paraphrased by third party); 2023 team note that AV1 HW encode needs Arc / RTX 4000 / RX 7000. — [jellyfin.org Intel HWA docs](https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/intel); [aicybr Jellyfin HW transcoding guide](https://aicybr.com/blog/jellyfin-hardware-transcoding-intel-nvidia-amd); [r/jellyfin nyanmisaka 2023](https://lr.in.psf.lt/r/jellyfin/duplicates/132fnbs)
- Plex: Shield/Android apps "not up to the task" of complex ASS; burn-in fallback quality suffers. — [Plex forum 446811](https://forums.plex.tv/t/nvidia-shield-android-app-ssa-ass-format-anime-subtitles-transcoding-problems/446811.md)
- Plex server: Jan 2025 forum reply said H.265 encoding during transcodes "is on its way", later post says hardware HEVC encode added to PMS; no evidence of AV1 transcoding in PMS; HDR tone mapping on Intel iGPU under Windows "still in active development". — [Plex forum AV1 transcode support](https://forums.plex.tv/t/av1-transcode-support/871570); [Plex forum HEVC on Intel 7th gen](https://forums.plex.tv/t/cant-transcode-hevc-on-intel-7th-gen/902038)
- Emby Android TV: libass JNI renderer fails on real ASS tracks (Feb–Apr 2025), transcode works. — [Emby topic 136069](https://emby.media/community/topic/136069-ass-subtitles-dont-render-on-android-tv)
- Stremio Android TV 1.9.3 (Feb 2026): user reports ASS subtitles lagged during anime openings; crashes on Android 8 TVs; subtitle-language toggle bug. — [r/Stremio 1.9.3 thread](https://redlib.groet-infra.nl/r/Stremio/comments/1qvcsjg/193_rocks/)
- Kyoo: no Android TV client (planned). Seanime: no Android TV client mentioned. — [zoriya/Kyoo](https://github.com/zoriya/Kyoo); [5rahim/seanime](https://github.com/5rahim/seanime)

### Inferences
- Direct play of 10-bit HEVC is a device-decoder question on all three servers (Shield/Google TV/Fire TV hardware), but only Jellyfin's ecosystem offers a client that also renders ASS without forcing the server to transcode; therefore Jellyfin + Thor (or 0.20.0) is the best Android TV anime stack found.
- Ordered chapters / segment linking was not addressed by any source for any server; treat as unsupported everywhere outside mpv/Kodi (not verified).

### Gaps
- No source found covering ordered-chapters (Matroska segment linking) support on any server/client.
- No source covering embedded-font attachment extraction on the official Jellyfin Android TV 0.20.0 beta.
- Plex's official HW-transcoding support article returned 403; HEVC-encode and tone-mapping status is from forum posts only.
- Emby codec matrix (AV1/VP9/Opus/passthrough on Android TV) not researched; Emby Premiere page confirms HW transcoding is a paid feature.
- Roku/webOS/Tizen/Fire TV availability per server not individually verified in this pass (Litefin covers Tizen/webOS for Jellyfin; Emby "Free Apps" list names Android, Fire TV, iOS, Xbox One, PS4).

---

## Key Question 6: Is there a current open-source project positioning itself as a "Jellyfin replacement" (Rust/Go) with active development in 2025–2026?

### Takeaway
Kyoo (Go/Python/React Native, GPL-3, single maintainer, v5 rewrite) is the only actively maintained general-purpose open-source server found that explicitly targets anime and does ASS with embedded fonts; Dim (Rust) and Olaris (Go) are effectively dormant, and Seanime (Go) is anime-specific rather than a general Jellyfin replacement.

### Cited Findings
- Kyoo: 2.5k stars, 5,972 commits on master, single developer, v5 rewrote most of the code; no plugin system by design; brings in extra containers (Meilisearch, separate transcoder). — [zoriya/Kyoo](https://github.com/zoriya/Kyoo); [selfhostyourself Kyoo](https://selfhostyourself.com/services/kyoo)
- Dim: Rust media server described in a 2026 roundup as "lightweight and fast, but early in development" with limited features and few clients; 2022 Linux Pro Magazine review called it early-stage; current activity not found. — [JellyWatch best free media server 2026](https://www.jellywatch.app/blog/best-free-media-server-software-2026-jellyfin-alternatives); [Linux Pro Magazine Dim review](https://www.linuxpromagazine.com/Issues/2022/260/Dim-Media-Manager); [dusk-labs/dim README](https://cdn.jsdelivr.net/gh/dusk-labs/dim@master/README.md)
- Olaris: no stable release since v0.4.0 (April 2022); no commits in 2023–2024; only minor maintenance patches late 2025 (per selfhosting.sh); AlternativeTo lists it discontinued with last commit July 2022. — [selfhosting.sh Olaris](https://selfhosting.sh/apps/olaris/); [AlternativeTo Olaris](https://alternativeto.net/software/olaris/about)
- Seanime: Go server, GPL-3, active, anime/manga focus. — [5rahim/seanime](https://github.com/5rahim/seanime)
- Jellyfin itself: 10.11.x line through 10.11.8 in 2026 with a 12.0 roadmap; 10.11.0 added full system backup. — [jellyfin v10.11.0 release](https://github.com/jellyfin/jellyfin/releases/tag/v10.11.0); [JellyWatch Jellyfin 10.11.7 / 12.0 roadmap](https://jellywatch.app/blog/jellyfin-current-version-latest-features-march-2026)

### Inferences
- No Rust/Go project has reached feature parity with Jellyfin's client ecosystem; Kyoo's lack of Android TV and arr/request app integration makes it a secondary/experimental option for an anime-on-TV household in 2026.

### Gaps
- Dim's GitHub commit activity in 2025–2026 was not verified (only README retrieved).
- Tsukimi, Reiverr, Nova, ErsatzTV-adjacent projects: no substantive results in this pass.

---

## Supplementary: Sonarr/Radarr library-refresh integrations

### Takeaway
Could not retrieve the servarr wiki Connections section (page body returned empty). Not verified in this pass.

### Cited Findings
- The servarr wiki Sonarr settings page exists but its body did not load in two fetch attempts. — [wiki.servarr.com/sonarr/settings](https://wiki.servarr.com/sonarr/settings)

### Inferences
- (None sourced.) Prior knowledge suggests Sonarr/Radarr "Connect" supports Plex Media Server, Emby/Jellyfin (shared connection type), and Kodi for library updates; this must be confirmed against the wiki before being reported as fact.

### Gaps
- Exact connection-type names and whether Jellyfin has its own entry vs. using the Emby connection in current Sonarr v4/Radarr v5 are unverified.
- Kyoo and Seanime have no documented Sonarr/Radarr connection; a webhook/custom-script would be needed (unverified).
