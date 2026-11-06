# TV clients and front-ends that render ASS/SSA with libass (Android TV / Google TV / Fire TV first; Apple TV, Roku, webOS, Tizen, desktop second) — status as of 2026-10-13

Scope note: dates on GitHub release pages often show only day/month; years below are inferred from release ordering (v0.19.10 = 16 Aug 2026, v0.20.0-beta.1 = 8 Sep 2026 per the releases page) and are flagged where inferred. Several comparison blogs cited here (swiftlyplayer.com, jellywatch.app) are written by developers of competing clients and are marked as such.

## Key Question 1: On Android TV, which players/front-ends actually use libass, and do they load embedded MKV font attachments?

### Takeaway
As of October 2026 the libass-capable options on Android TV are: Kodi (any Jellyfin/Plex/Emby add-on; libass is Kodi's subtitle renderer since v20 and it extracts MKV font attachments), the official Jellyfin for Android TV 0.20 beta (ASS direct play via the `ass-media` Media3 extension from libass-android), Wholphin (ExoPlayer+libass-android or mpv), Streamyfin (mpv), Moonfin (Media3-based; ASS handling unverified), mpv-android, and VLC (libass but with long-standing font problems). Findroid has no Android TV build; Plex and Nova do not do styled ASS; Emby's app has an internal libass renderer with reported bugs. Embedded-font loading is documented for Kodi only; no other Android TV client documents it.

### Cited Findings

**Official Jellyfin for Android TV (jellyfin-androidtv)**
- v0.19.0 (released 28 Oct, year inferred 2025) changelog contains both "Add experimental ASS direct play using libass" (#4630), "Use overlay rendering for experimental SSA support" (#4685), "Remove libass preference in non-development builds" (#4876) and "Remove libass library" (#5032) — i.e. libass was added experimentally and then removed before stable 0.19.0 shipped — [jellyfin-androidtv v0.19.0 release](https://github.com/jellyfin/jellyfin-androidtv/releases/tag/v0.19.0)
- Issue #4922 "[v0.19.0-beta.1] ASS subtitle codec is not supported" was closed as not planned — [jellyfin-androidtv #4922](https://github.com/jellyfin/jellyfin-androidtv/issues/4922)
- Jellyfin forum moderators (late 2024/early 2025) explained: "ASS sub support was not removed intentionally, the experimental video player [libVLC] that could direct play it was... Support for ASS subtitles will come in the future (no estimated timeframe)" and that on 10.10 ASS tracks caused transcode to H.264 "because ASS subtitles are not supported by exoplayer" — [Jellyfin forum: will the Android TV app ever support ASS](https://forum.jellyfin.org/t-will-the-jellyfin-android-tv-app-ever-support-ass-subtitles); a Jellyfin developer reply in late 2024 said "there is no Android compatible build of libass. This means that exoplayer/media3 doesn't support rendering .ass currently" — [Jellyfin forum thread](https://forum.jellyfin.org/t-will-the-jellyfin-android-tv-app-ever-support-ass-subtitles?pid=44484)
- v0.20.0-beta.1 (08 Sep 2026) highlights: "Support Advanced SubStation Alpha direct play" (#5121, by @nielsvanvelzen); "Add option to always burn in subtitles when transcoding" (#4459); "Add user preferences to force enable or disable common HDR formats" (#5743); "Allow Shield Dolby Vision Profile 7 direct playback" (#5652); "Refactor: Extract HEVC and AV1 codec modules" (#5646); dependency updates: androidx.media3 v1.11.0 (#5741), org.jellyfin.media3:media3-ffmpeg-decoder v1.9.0+1 (#5288), and **io.github.peerless2012:ass-media v0.5.0 (#5723) / v0.5.1 (#5750)** — [jellyfin-androidtv v0.20.0-beta.1](https://github.com/jellyfin/jellyfin-androidtv/releases/tag/v0.20.0-beta.1)
- v0.20.0-beta.3 (24 Sep 2026): "Implement auto-detection for audio passthrough and allow overrides"; "Increase maximum subtitle size to 48". v0.19.5: "Add AV1 detection support for older Android versions". v0.19.10 (16 Aug 2026, marked Latest stable): "Add Amazon Fire TV 4K (1st Gen) to modelsWithDoViHdr10PlusBug" — [jellyfin-androidtv releases](https://github.com/jellyfin/jellyfin-androidtv/releases)
- libass-android's README lists "Jellyfin Android TV" among apps using it (alongside FongMi/TV, AfuseKt, Fladder, NuvioTV) — [peerless2012/libass-android](https://github.com/peerless2012/libass-android)
- A third-party comparison (written by the Swiftly Player developer) states the official app uses "Media3 ExoPlayer 1.11.1 with Jellyfin's FFmpeg decoder" and handles ASS/SSA "via the FFmpeg decoder and libass"; lists v0.19.10 stable (16 Aug 2026) and v0.20.0-beta.2 (Sep 2026) — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)
- No source found that states whether the official app's ASS path loads MKV font attachments (gap below).

**libass-android (the enabling library for Media3 clients)**
- libass-android (MIT, by peerless2012) ships three modules: `lib_ass` (native libass.so + headers), `lib_ass_kt` (Kotlin wrapper), and `lib_ass_media` ("a Media3 extension for libass" that lets Media3 players render libass subtitles); 89 stars, 201 commits; the README does not mention MKV font attachments, a libass version, or performance — [peerless2012/libass-android](https://github.com/peerless2012/libass-android)
- A maintainer-adjacent comment on the androidx/media PR (18 Sep 2026) says: "Since this pr is not plan to merge, we recommend to use libass-android to make your media3 support ASS/SSA sub" — [androidx/media PR #2324](https://github.com/androidx/media/pull/2324)

**Wholphin (Jellyfin-only Android TV client)**
- README: Jellyfin only, requires server 10.10.x or 10.11.x; Android TV and Fire TV OS (Android 6+/Fire OS 6+); two engines: ExoPlayer "with optional extra audio, SSA/ASS, and AV1 software decoding" and MPV which "direct plays anything" with strong SSA/ASS support; subtitle styling for plain-text subs with separate HDR settings; optional auto refresh-rate/resolution switching; GPL-2.0 — [damontecres/Wholphin](https://github.com/damontecres/Wholphin)
- Issue #1049 (opened 6 Mar 2026): after the developer "added libass-android to improve SSA/ASS subtitle support", ASS subtitles did not appear at all in ExoPlayer direct playback (hardware overlay), while fallback mode showed them without styling/karaoke/positioning; MPV path had intermittent garbled accented characters; developer reverted while investigating; closed 11 May 2026 via commit 96f94fe / PR #1362 described as "a fix for a libass-android lifecycle issue" addressing "much of" the problem — [Wholphin #1049](https://github.com/damontecres/Wholphin/issues/1049)
- Swiftly's comparison lists Wholphin v1.0.8 (14 Sep 2026) with "MPV is used for Direct Play of SSA/ASS files; ExoPlayer has optional software SSA/ASS decoding" — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/); JellyWatch's (competitor-written, June 2026) table instead claims Wholphin requires burn-in for ASS and has only "partial" passthrough, with a reader comment (6 Oct 2026) reporting clean passthrough — [jellywatch.app comparison](https://jellywatch.app/blog/jellywatch-tv-moonfin-wholphin-streamyfin-best-jellyfin-client-2026) (conflicts with Wholphin's own README)

**Streamyfin**
- Streamyfin PR #2055 (open at time of research) states Android "uses consistent MPV/libass viewport margin settings for phone, TV and PiP" and that "explicit ASS positioning and event-level margins retain authored placement" — [streamyfin PR #2055](https://github.com/streamyfin/streamyfin/pull/2055)
- Latest release v0.54.1 (2 Jun 2026); TV APK distributed via GitHub releases; MPV engine — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/); JellyWatch lists Streamyfin as MPV-based, ASS Direct Play (no burn-in), open source, iOS+Android — [jellywatch.app comparison](https://jellywatch.app/blog/jellywatch-tv-moonfin-wholphin-streamyfin-best-jellyfin-client-2026)

**Findroid**
- Store listing: "With the built-in mpv player you are sure that all media formats will play correctly including styled SSA/ASS subtitles"; GitHub README lists mpv subtitle support as SRT, VTT, SSA/ASS, DVDSUB and notes ExoPlayer has limited SSA/ASS styling; v1.1.0 changelog: separate mpv settings, mpv.conf/input.conf editor, "updated mpv and use system fonts" — [Findroid on IzzyOnDroid](https://apt.izzysoft.de/packages/dev.jdtech.jellyfin/); [jarnedemeulemeester/findroid](https://github.com/jarnedemeulemeester/findroid)
- Findroid lists Android TV as *planned*; v1.1.0 (18 Jul 2026) has no TV build, so it is not currently an Android TV option — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)

**Moonfin (Jellyfin + Emby)**
- Moonfin-Client organisation describes "enhanced Jellyfin and Emby clients"; the v2.0.0 release notes say the Android TV build uses "ExoPlayer/Media3 playback, audio passthrough", with leanback integration; supports Android TV 6.0+, Shield, Fire TV, Chromecast with Google TV; repo being renamed Moonfin-Core to power mobile, desktop, Android TV, Tizen and web — [Moonfin-Client](https://github.com/Moonfin-Client); [Moonfin tvOS 2.0.0 release notes](https://newreleases.io/project/github/Moonfin-Client/tvOS/release/2.0.0)
- Contradiction: JellyWatch's comparison says Moonfin is "MPV-based", "not open source", iOS/Android — [jellywatch.app comparison](https://jellywatch.app/blog/jellywatch-tv-moonfin-wholphin-streamyfin-best-jellyfin-client-2026); Swiftly's says "Native Media3 / ExoPlayer on Android TV", v2.5.1 (28 Aug 2026), Dolby Vision Profile 7 direct play, passthrough listed — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/). Moonfin's own notes side with Media3. ASS/libass handling not stated by any source.

**JellyWatch TV (closed source, freemium)**
- Vendor claims: ExoPlayer + FFmpeg extensions, ASS/SSA "pixel-perfect via libass", HDR10/HDR10+/Dolby Vision, passthrough chain "force passthrough → local FFmpeg decode → server transcode"; Android TV/Google TV/Fire TV; not open source — [jellywatch.app comparison (vendor-written)](https://jellywatch.app/blog/jellywatch-tv-moonfin-wholphin-streamyfin-best-jellyfin-client-2026)

**Kodi (with Jellyfin for Kodi, JellyCon, PlexKodiConnect, Emby for Kodi)**
- Kodi 20 "Nexus" moved most subtitle rendering to libass; local font files in `userdata/media/Fonts` are searched and used — [Kodi forum: new subtitles renderer in Kodi 20](https://forum.kodi.tv/showthread.php?tid=364572)
- xbmc PR #19535 (merged 10 Apr 2021, milestone Nexus 20.0 Alpha 1, backported in #19563): because "libass only supports a single folder for extra fonts", Kodi's demuxer now extracts fonts muxed into the video (MKV attachments) into the user font directory with a `tmp.font.` prefix and cleans them up later; fixes #16209 "External ASS subtitles with custom fonts always display with default font" — [xbmc PR #19535](https://github.com/xbmc/xbmc/pull/19535)
- Kodi has an "Override ASS/SSA subtitle fonts" setting; users are advised to keep it off for authored styling; a CoreELEC user with ~4,000 fonts in the fonts folder saw ~2 minutes of CPU before each ASS episode attributed to libass scanning — [OSMC forum](https://discourse.osmc.tv/t/problem-while-playing-custom-font-captions/77525); [CoreELEC forum](https://discourse.coreelec.org/t/external-ass-and-external-fonts-performance/15715)
- Kodi 22 "Piers" is at RC/beta stage (10 Oct 2026) with HDR overlay support incl. UHD PGS and HDR-to-SDR tone mapping of PGS on Windows — [deskmodder, 10 Oct 2026](https://www.deskmodder.de/blog/2026/10/10/kodi-22-0-piers-kann-ausprobiert-werden/); [androidpctv Piers Beta 2](https://androidpctv.com/kodi-22-piers-beta2/)
- Jellyfin for Kodi targets "current release±1 ... Nexus (old), Omega (current) and Piers (next)" — [jellyfin/jellyfin-kodi](https://github.com/jellyfin/jellyfin-kodi); latest Jellyfin for Kodi v2.2.0 (17 Sep 2026), JellyCon v1.0.2 (6 Jul 2026) — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)
- Jellyfin's codec matrix lists Kodi as supporting HEVC 8/10-bit, H.264 10-bit, AV1 and VP9 — [Jellyfin codec support](https://jellyfin.org/docs/general/clients/codec-support/)

**mpv-android**
- README lists "libass support for styled subtitles" as a feature; based on libmpv — [mpv-android/mpv-android](https://github.com/mpv-android/mpv-android)

**VLC for Android / Android TV**
- VLC Android bug reports: CJK ASS subtitles rendered as placeholder boxes on a Sony Android TV while SRT worked (v2.5.2) — [vlc-android issue 381](https://code.videolan.org/videolan/vlc-android/-/issues/381); fonts embedded as MKV attachments are "what VLC can handle fine" but fonts embedded inside a separate .ass file are not picked up (muxing into MKV fixed it) — [vlc-android issue 1618](https://code.videolan.org/videolan/vlc-android/-/issues/1618); a ticket asks for a user-selectable font folder for libass as MX Player has — [VLC forum](https://forum.videolan.org/viewtopic.php?p=422655). No 2026 status on VLC Android's libass version was found.

**Nova Video Player**
- Developer FAQ: SRT, SUB, VobSub, PGS supported; "SSA support is rudimentary" — [Nova FAQ](https://home.courville.org/nova_video_player-faq/index.html)

**Just Player / Next Player**
- Just Player: packs FFmpeg "for some advanced features", ICU4J for subtitle charset detection; nothing about libass — [Just Player GitHub (mirror)](https://github.com/shooter7/Player); Next Player: described only as a Kotlin native Android player; no subtitle/libass details found — [AlternativeTo: Next Player](https://alternativeto.net/software/next-player)

**Stremio (Android TV)**
- July 2024 update moved the internal player to Media3 (ExoPlayer) 1.4.0-alpha02, fixed a libVLC audio delay, and moved subtitles to "a software layer"; ASS styling not described — [AlternativeTo news, Jul 2024](https://alternativeto.net/news/2024/7/stremio-enhances-android-tv-experience-with-media3-update-and-bug-fixes)

**Plex (Android TV)**
- Plex's Android TV settings: "Burn Subtitles" has three modes; Automatic (default) burns in image-based subtitles **and complex ASS/SSA**; "Only image formats" limits burn-in to image subs; "Always" burns everything — [Plex support: Settings Android TV](https://support.plex.tv/articles/settings-android-tv/); a 2018 forum thread confirms ASS plays "natively" (unstyled) under "Only image formats" — [Plex forum](https://forums.plex.tv/t/new-option-burn-subtitles-avoid-at-all-costs/320968)

**Emby (Android TV)**
- Emby Community thread (Feb–Apr 2025): ASS subs in MKV failed to display during Direct Play on Android TV and Android (app 3.4.56) while SRT worked; staff said the app uses an internal renderer and logs showed `libass_jni` "Empty event"; Luke: "We can look at updating the libass version it is using"; thread ended without a fix — [Emby Community #136069](https://emby.media/community/topic/136069-ass-subtitles-dont-render-on-android-tv); older thread: Emby for Android "has got a modified version of Exoplayer that renders ASS subtitles" — [Emby Community #91995](https://emby.media/community/topic/91995-shield-tv-and-ass-subtitles); a 2019-era thread reported Emby's ASS burn-in taking 20s+ vs ~2s on Plex with HW accel — [Emby Community #86635](https://emby.media/community/topic/86635-ssa-subtitles-lengthy-burn-in-compared-to-plex-or-not-playing-at-all/)

**Swiftly Player**
- libVLC on Android; 1.0.3 on Apple/Windows/Linux, Android "in closed test"; author-written article — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)

### Inferences
- Practical Android TV matrix (libass = full-styled ASS incl. typesetting/karaoke):
  - Kodi + Jellyfin for Kodi / JellyCon / PlexKodiConnect / Emby for Kodi: libass yes (Kodi's own renderer since v20); MKV font attachments yes (extracted to user font dir since Nexus); backends Jellyfin/Plex/Emby via add-ons; HW decode via Kodi (MediaCodec); passthrough yes.
  - Jellyfin for Android TV 0.20 beta: libass yes via `ass-media` (libass-android); font attachments undocumented; Jellyfin only; Media3 1.11 + Jellyfin FFmpeg decoder; HDR/DoVi P7 on Shield; passthrough auto-detect (beta.3). Stable 0.19.10 still has no ASS direct play (ASS triggers transcode/burn-in).
  - Wholphin: libass yes (ExoPlayer + libass-android, buggy until May 2026 fix) or mpv; Jellyfin only; Android TV + Fire TV.
  - Streamyfin: mpv/libass yes; Jellyfin only; TV APK via GitHub.
  - Moonfin: Media3 (per project), ASS path unverified; Jellyfin + Emby.
  - JellyWatch TV: vendor claims libass; closed source; Jellyfin only.
  - mpv-android: libass yes; no server integration (generic player; usable as external player from Jellyfin/Kodi apps).
  - VLC: libass yes but font handling flaky; generic/external player.
  - Findroid: mpv yes but no Android TV build.
  - Plex app: no — complex ASS is burned in by default. Emby app: internal libass renderer with bugs. Nova/Just/Next Player/Stremio: no credible styled-ASS support.
- Because Media3's own SSA support is cue-based (see Q2), any Android TV client not using mpv/libVLC/libass-android (Plex, Nova, Just Player, Next Player, Stremio's Media3 path, Moonfin unless it added ass-media) should be assumed to drop typesetting, positioning and karaoke.
- Kodi is the only option where MKV font-attachment loading is documented in merged source; for every other Android TV client, assume embedded fonts may not be honoured unless verified locally.

### Gaps
- No source confirms whether `ass-media`/libass-android (and therefore Jellyfin Android TV 0.20, Wholphin) loads MKV font attachments or only system fonts.
- No stable (non-beta) Jellyfin Android TV release with ASS direct play existed on 13 Oct 2026; 0.20.0 final release date unknown.
- Moonfin's actual ASS rendering path could not be verified from source; two comparison blogs contradict each other.
- Yatse (Kodi remote) and "Fintunes" (Jellyfin music client): no relevant sources found; Fintunes is a music client and out of scope for subtitles.
- Just Player and Next Player: no documentation on ASS styling found; assumed Media3-cue-only.
- Stremio: no source on whether ASS styling survives its software subtitle layer.
- Nova: FAQ says rudimentary SSA; no newer source.

## Key Question 2: Does ExoPlayer/Media3 have ASS rendering support, and what is the state of libass-android / "media3-libass" / media3 SSA integration?

### Takeaway
Media3/ExoPlayer has only a cue-based SSA parser (basic text, no typesetting), the upstream libass PR was declared "wont merge" by Google (April 2025), and the de-facto solution is the third-party libass-android `lib_ass_media` extension, now consumed by Jellyfin Android TV 0.20 betas and Wholphin.

### Cited Findings
- google/ExoPlayer issue #8435 "Support SSA/ASS styling" (open since 2021): discussion notes "Libass creates just another 'video' stream based on description of text file," which is hard to fit into Media3's cue model — [ExoPlayer #8435](https://github.com/google/ExoPlayer/issues/8435)
- androidx/media PR #2324 "Add libass native decoder" (opened ~10 Apr 2025 by moi15moi et al.): adds surface/video-size/colorspace plumbing for text renderers, MKV font attachment parsing, a `decoder_ass` module and VSFilter-mangled-colour conversion; listed problems: only ASS muxed in MKV, hacky hooks in `SubtitleTranscodingTrackOutput`/`DefaultRenderersFactory`, frame-accuracy depends on #2289, `draw_ass_rgba` too slow for 4K bitmaps (needs NEON/OpenMP). Maintainer icbaker (24 Apr 2025): "we are unlikely to merge this PR as the maintenance burden is likely to be too high for the benefit it brings." Labeled draft/"wont merge"; authors stopped work. peerless2012 (18 Sep 2026) recommends libass-android instead — [androidx/media PR #2324](https://github.com/androidx/media/pull/2324)
- androidx/media issue #2384: MKV tracks with codec ID `S_TEXT/SSA` were ignored on Media3 1.6.1 (never surfaced as text/x-ssa) — [androidx/media #2384](https://github.com/androidx/media/issues/2384)
- libass-android: modules `lib_ass`, `lib_ass_kt`, `lib_ass_media` (Media3 extension); MIT; users listed include Jellyfin Android TV, FongMi/TV, AfuseKt, Fladder, NuvioTV — [peerless2012/libass-android](https://github.com/peerless2012/libass-android); the Maven artifact consumed by Jellyfin is `io.github.peerless2012:ass-media` v0.5.1 (Sep 2026) — [jellyfin-androidtv v0.20.0-beta.1](https://github.com/jellyfin/jellyfin-androidtv/releases/tag/v0.20.0-beta.1)
- Alternative community project "assrender": extracts raw ASS from the Media3 extraction pipeline and renders with libass in an overlay view "with full styling, fonts, and animations", avoiding FFmpeg — [jepsefan/assrender](https://github.com/jepsefan/assrender)
- Wholphin's integration of libass-android initially broke ASS entirely in hardware-overlay direct playback (Mar 2026) and required a "libass-android lifecycle" fix (May 2026) — [Wholphin #1049](https://github.com/damontecres/Wholphin/issues/1049)
- A pure-Rust ASS renderer ("rustyass") to replace libass was proposed in tinystream issue #52 — [tinystream #52](https://github.com/tinystream-dev/tinystream/issues/52) (proposal only)

### Inferences
- There is no official "media3-libass"; the term in the wild maps to `lib_ass_media` / `io.github.peerless2012:ass-media`. Any Media3 app wanting full ASS must bolt on libass-android (or assrender) and handle surface/overlay lifecycle itself — the same class of bug Wholphin hit.
- Media3's built-in SSA decoder remains cue/text-only; "basic ASS support" claims from Media3 apps mean dialogue text without typesetting.
- The "4K bitmap too slow" note in PR #2324 suggests libass CPU/blit cost on 4K output is a real concern on low-end SoCs (see Q6).

### Gaps
- No libass version number or release tag for libass-android found (Releases page empty in fetch).
- Whether `lib_ass_media` parses MKV font attachments (as PR #2324 did) is undocumented.
- No performance benchmarks for libass-android on Android TV SoCs found.

## Key Question 3: Apple TV — Infuse, Swiftfin, Plex, Emby: which do libass?

### Takeaway
Swiftfin's VLCKit player path renders ASS (VLC uses libass) but its native AVPlayer path burns in; a native-player ASS overlay renderer is an open, "Blocked" roadmap item. Infuse renders authored ASS styling with its own engine but community reports note positioning/typesetting weaknesses and no custom font folder; no source confirms libass. No sources found for Plex/Emby on tvOS.

### Cited Findings
- Swiftfin issue #1892 (opened 6 Jan 2026, open, roadmap "Blocked"): AVPlayer's native subtitle support is limited; "Users who want ASS subtitles must use the VLC-based player," which a user says drains battery, handles HDR/DoVi poorly and lacks Atmos; proposal to use the open-source Swift ASS Renderer over AVPlayer. Maintainer JPKribs (21 Apr 2026): unsupported formats are burned in on the native player; custom ASS rendering is blocked behind HLS/track fixes (PR #1882), tvOS UI (PR #1902) and migration to `AVMediaPlayerProxy` (#1853); 17 Jul 2026: pointed to PR #2046 as "third in the queue" — [Swiftfin #1892](https://github.com/jellyfin/Swiftfin/issues/1892)
- Older Swiftfin tvOS reports: embedded MKV subs not showing (fixed by "Force Direct Play"); one commenter "no issues with series with ASS subtitles"; with native player enabled subtitle menu shows only Off/Auto/CC — [Swiftfin #729](https://github.com/jellyfin/Swiftfin/issues/729); [Swiftfin #954](https://github.com/jellyfin/Swiftfin/issues/954)
- Jellyfin codec matrix: Swiftfin iOS supports HEVC 8/10-bit, H.264 10-bit, AV1 (on by default with VLCKit, off with AVKit), VP9 software via VLCKit only — [Jellyfin codec support](https://jellyfin.org/docs/general/clients/codec-support/)
- Infuse: supported subtitle formats include SSA/ASS; release notes mention a fix for "some Chinese ASS subtitles with unavailable fonts" and "optimized memory allocation for .ASS subtitles" — [Infuse App Store](https://apps.apple.com/us/app/infuse/id1136220934); [firecore.com/infuse](https://firecore.com/infuse)
- Firecore staff: "SSA/ASS subtitles have embedded styles which cannot be overridden in Infuse at this time" — [Firecore: Disable ASS/SSA styling](https://community.firecore.com/t/disable-ass-ssa-styling/14465); Dec 2025 feature request confirms Infuse still offers no styling override for ASS and renders "as authored" — [Firecore feature request, Dec 2025](https://community.firecore.com/t/feature-request-accessibility-option-for-overriding-ass-subtitle-styles/58387.md)
- Community reports: heavily typeset anime signs render "choppily" vs mpv-based players; ASS positioning tags reported unsupported (older thread); ASS vertical position based on video height not screen height on letterboxed content (2017); request for a custom font folder for ASS "like nPlayer/zFuse" because missing fonts break typesetting — [Firecore: subtitles not rendering properly (.ass)](https://community.firecore.com/t/subtitles-not-rendering-properly-ass/35367); [Firecore: position tag support](https://community.firecore.com/t/subtitles-position-tag-support/10355); [Firecore: custom font support request](https://community.firecore.com/t/please-add-custom-font-support-for-ass-subtitles-like-nplayer-zfuse/60192)

### Inferences
- On Apple TV the only path with libass-grade fidelity is Swiftfin's VLCKit player (libVLC bundles libass), at the cost of HDR/DoVi/Atmos handling per user reports. Infuse is "good enough" for styled dialogue but not a libass-equivalent for signs/karaoke; Plex/Emby tvOS apps use AVPlayer-class playback and should be assumed to burn in (unverified).

### Gaps
- No source found describing Plex or Emby tvOS ASS handling.
- No official Firecore statement on current typesetting/positioning fidelity or libass usage; community reports span 2016–2025.

## Key Question 4: Roku / webOS / Tizen — is proper ASS rendering possible, or only burn-in?

### Takeaway
On Roku, Jellyfin burns ASS in (DeliveryMethod Encode) and nobody has established that Roku can render a muxed ASS track; Tizen 5 reports show ASS forcing transcode/burn-in; for webOS only forum hearsay exists. In browsers, jellyfin-web uses a libass-wasm (JavascriptSubtitlesOctopus fork) renderer, but no source confirms the Tizen/webOS wrappers run it.

### Cited Findings
- jellyrock (Roku Jellyfin client) research issue #1008 (23 Sep 2026, open, "not an agreed change"): server currently burns ASS/SSA (`DeliveryMethod: Encode`) because `ass` is not in the advertised subtitle profile; the server will not externalize ASS "because ASS styling and positioning can't be represented in WebVTT"; adding an explicit `ass` Embed entry flipped the answer to Embed in tests; 340 of 600 scanned items had embedded `ass`; "it is unknown whether Roku renders a muxed ASS track at all, or how well"; PGS/DVDSub stay Encode — [jellyrock #1008](https://github.com/jellyrock/jellyrock/issues/1008)
- Jellyfin forum (2025): Roku client did not pass along the burn-in request; tracked in jellyfin-roku #347 — [Jellyfin forum: Subtitle burn-in on Roku](https://forum.jellyfin.org/t-subtitle-burn-in-on-roku-client); jellyfin-roku #526: subtitles burned in on transcode even when not enabled — [jellyfin-roku #526](https://github.com/jellyfin/jellyfin-roku/issues/526)
- jellyfin-tizen #103 (Tizen 5): ASS triggered transcode with reason "subtitle codec not supported"; forcing burn-in made playback work; same TV played ASS directly from USB — [jellyfin-tizen #103](https://github.com/jellyfin/jellyfin-tizen/issues/103)
- Jellyfin docs: if subtitles need transcoding they are converted or burned in; burn-in is the most CPU-intensive transcode; Roku supports HEVC 8/10-bit only on 4K devices, AV1 ✓; webOS HDR supported but MKV causes issues (recommend force remux MKV→MP4); webOS/Tizen absent from the codec table — [Jellyfin codec support](https://jellyfin.org/docs/general/clients/codec-support/)
- A fork of jellyfin-web depends on a JavascriptSubtitlesOctopus (libass-wasm) fork; JASSUB is a libass→WASM wrapper with WebGL acceleration; litefin (third-party client) "implements libass-wasm .ass subtitles renderer used in jellyfin-web" — [repos.ecosyste.ms libass-wasm usage](https://repos.ecosyste.ms/usage/npm/libass-wasm?page=2); [jassub on npm](https://npmjs.com/package/jassub); [litefin v1.3.0](https://newreleases.io/project/github/MoazSalem/litefin/release/v1.3.0)
- Forum recollections: "the Jellyfin WebOS app is dependent on WebOS's browser player, IIRC" and "WebOS will directly render ASS subtitles" — unverified — [Jellyfin forum thread](https://forum.jellyfin.org/printthread.php?tid=5349)
- Plex documentation lists SSA/ASS as "fully supported", i.e. compatible with all Plex apps including those that require transcoding (i.e. via burn-in) — [Plex: Adding local subtitles](https://support.plex.tv/articles/200471133-adding-local-subtitles-to-your-media/)
- Jellyfin Media Player (desktop, mpv-based) and mpv-based clients are the recommended route for ASS rendering in a 2026 Jellyfin subtitles guide — [JellyWatch subtitles guide 2026](https://jellywatch.app/blog/jellyfin-subtitles-complete-guide-2026)

### Inferences
- For Roku: plan on server-side burn-in (ffmpeg with libass on the server), which gives correct typesetting but costs a transcode per stream. Treat the Embed path as experimental.
- For Tizen/webOS: since the official apps wrap jellyfin-web, a libass-wasm renderer is technically present in the web code, but the Tizen issue shows the apps advertise profiles that make the server transcode. Expect burn-in unless a per-client profile change lands.
- "Proper" (client-side, toggleable, lossless) ASS on these platforms is not reliably available in Oct 2026; burn-in is the dependable method.

### Gaps
- No primary source on the current Jellyfin webOS app's ASS behaviour or whether jellyfin-web's libass-wasm path is active in the webOS/Tizen packages.
- Plex/Emby native Roku/webOS/Tizen apps: no sources beyond Plex's "fully supported (via transcode)" documentation.
- Whether Roku renders muxed ASS at all is explicitly unknown per jellyrock.

## Key Question 5: Kodi + Jellyfin add-on — user accounts, watched-state sync, and interplay with Sonarr / Radarr

### Takeaway
Jellyfin for Kodi is single-user per Kodi profile (multi-user = one Kodi profile per Jellyfin user), syncs watched/resume state two-way in near real time via websocket plus the server-side Kodi Sync Queue plugin, but has a history of watched-state regressions; JellyCon is a thin, multi-user-switchable alternative. Sonarr/Radarr-triggered Jellyfin library refreshes propagate to Kodi via the same live sync.

### Cited Findings
- Jellyfin docs: Add-on mode (default; server mediates, works LAN/remote, Kodi direct plays nearly everything) vs Native mode (Kodi reads NFS/SMB directly, LAN/VPN only, needs path substitution). "The add-on doesn't natively handle multiple accounts"; recommended approach is Kodi profiles, with the add-on configured per profile; JellyCon "makes it easier to switch between multiple servers or users" since it doesn't sync metadata. Startup sync asks the server's Kodi Sync Queue plugin for items changed since last shutdown; while running, "the server sends a websocket notification when an item changes" (real-time, not polling). Caveats: MySQL shared DBs unsupported (SQLite only); conflicts with other tools touching Kodi's DB; don't copy jellyfin.db between clients; install Kodi Sync Queue to avoid waiting for periodic re-syncs; default skin lacks full Unicode fonts — [Jellyfin docs: Kodi](https://jellyfin.org/docs/general/clients/kodi)
- jellyfin-kodi README: "2-way watched and resume state between your server and Kodi. This is a near instant feature"; supports Nexus/Omega/Piers; "native playback mode" needed for chapter images and some add-ons — [jellyfin/jellyfin-kodi](https://github.com/jellyfin/jellyfin-kodi)
- JellyCon README: "a thin frontend for a Jellyfin server"; "can easily switch between multiple user accounts at will"; Quick Connect, server auto-detect, manual login — [jellyfin/jellycon](https://github.com/jellyfin/jellycon)
- jellyfin-kodi issue #645 ("Users added to the session" / permanent users not working): "only the user actually logged into the addon will have their watch-status updated" (~1,600 days old; no fix confirmation found) — [jellyfin-kodi #645](https://github.com/jellyfin/jellyfin-kodi/issues/645)
- Watched-state bugs: #210 (Kodi marks watched then server state overwrites to unwatched moments later; forced full sync reverts to server state), #606 ("Mark as watched" not synced), #932 (watch state not synced when using an external player) — [jellyfin-kodi #210](https://github.com/jellyfin/jellyfin-kodi/issues/210); [#606](https://github.com/jellyfin/jellyfin-kodi/issues/606); [#932](https://github.com/jellyfin/jellyfin-kodi/issues/932)
- Community gist: Sonarr/Radarr "Connections" custom script that calls Jellyfin's library refresh endpoint on import — [gist by madiele](https://gist.github.com/madiele/fda35bce6e67c19764728174d0695a03)
- Jellyfin for Kodi v2.2.0 released 17 Sep 2026; JellyCon v1.0.2 6 Jul 2026 — [swiftlyplayer.com comparison](https://swiftlyplayer.com/blog/jellyfin-android-tv-clients-compared/)
- Kodi + external player: watched state not synced via Jellyfin for Kodi when an external player (e.g. mpv-android) is used — [jellyfin-kodi #932](https://github.com/jellyfin/jellyfin-kodi/issues/932)

### Inferences
- Chain for anime: request → Sonarr/Radarr grab → import → Jellyfin library refresh (Jellyfin's own real-time monitor or a Sonarr connection script/webhook) → Kodi Sync Queue records the change → Jellyfin for Kodi receives websocket update or picks it up at next startup sync. Kodi never talks to Sonarr/Radarr directly; nothing in this chain requires Kodi-specific configuration beyond installing Kodi Sync Queue.
- Per-user accounts on a shared living-room Kodi are clunky (Kodi profile switching); JellyCon or the native Jellyfin Android TV/Wholphin/Streamyfin apps are better for frequent user switching. Watched-state from Kodi only flows back for the logged-in Jellyfin user.
- Playing through an external player from Kodi (to get mpv's libass) sacrifices watched-state sync — a direct trade-off against Kodi's own libass, which is already full-featured.

### Gaps
- No source confirms whether the "permanent users" bug (#645) is fixed in Jellyfin for Kodi 2.x.
- No source on whether Jellyfin's built-in real-time library monitoring is sufficient without a Sonarr connection script (depends on server filesystem/inotify, not researched).
- PlexKodiConnect and Emby for Kodi account/watched-state behaviour were not researched (no sources gathered).

## Key Question 6: Recommended Android TV hardware for anime (Shield TV Pro, Google TV Streamer, Onn 4K Pro, Fire TV Stick 4K Max) — HEVC 10-bit, AV1, libass CPU cost

### Takeaway
All four decode HEVC; AV1 hardware decode is confirmed for Google TV Streamer (MT8696), Onn 4K Pro (S905X4) and Fire TV Stick 4K Max 2nd gen (MT8696T), while sources conflict on the Shield (Tegra X1+, widely reported as lacking AV1 hardware). The Shield remains the only one with TrueHD/DTS passthrough and the strongest CPU/GPU; no source quantifies libass CPU cost per device, but Media3 PR notes and Kodi forum reports show 4K ASS rasterisation and large font folders are CPU-sensitive, favouring the Shield for heavy typesetting.

### Cited Findings
- JellyWatch 2026 device comparison (prices as of Mar 2026; competitor-written): Shield TV Pro — Tegra X1+, HDR10 ✓, HDR10+ ✗, Dolby Vision falls back to HDR10 for local Jellyfin files, TrueHD ✓, DTS ✓, HDMI 2.0b, ~$200, AV1 marked ✓; Onn 4K Pro — AV1 ✓, HDR10/HDR10+/DV ✓, TrueHD ✗, DTS ✗, Atmos as lossy DD+, 3 GB RAM, HDMI 2.0, ~$50; Fire TV Stick 4K Max — MediaTek MT8696T, AV1 ✓, HDR10/HDR10+ ✓, DV unreliable for local playback, TrueHD ✗, DTS ✗, Atmos capped at DD+, ~$60 — [JellyWatch device comparison](https://jellywatch.app/blog/best-streaming-device-jellyfin-4k-hdr-av1-2026)
- Conflicting: another 2026 guide states "Nvidia Shield 2019 (Tegra X1+) — no AV1 hardware" — [dualiptv AV1 device status 2026](https://www.dualiptv.com/guide/av1-iptv-2026-status-across-devices); AFTVnews: Fire TV Stick 4K Max is "the only stand-alone Fire TV model that supports AV1 hardware video decoding" (MT8696) — [AFTVnews AV1 list](https://www.aftvnews.com/list-of-fire-tvs-fire-tv-sticks-and-fire-tv-smart-tvs-that-support-av1-video-decoding/); Emby thread reports AV1 transcoding on a Firestick 4K Max (client profile issue) — [Emby Community #112624](https://emby.media/community/topic/112624-firestick-4k-max-av1-is-transcoding/)
- Onn 4K Pro chip is Amlogic S905X4 with full AV1 — [dualiptv AV1 status](https://www.dualiptv.com/guide/av1-iptv-2026-status-across-devices); Jellyfin forum thread on AV1 direct play to S905X4 — [Jellyfin forum](https://forum.jellyfin.org/t-solved-av1-directplay-to-amlogic-s905x4-not-working)
- Google TV Streamer: MediaTek MT8696, quad-core Cortex-A55 up to 2 GHz (TechInsights measured 1.8 GHz), PowerVR GE9215 GPU, 4 GB RAM/32 GB storage, video codecs AV1/VP9/H.264/H.265, HDR10/HDR10+/Dolby Vision/HLG up to 4K60, HDMI 2.1, USB-C OTG, Gigabit Ethernet, Wi-Fi 5 — [androidtv-guide.com](https://androidtv-guide.com/streaming-gaming/google-tv-streamer); [Android Central chip report](https://androidcentral.com/streaming-tv/turns-out-the-new-google-tv-streamer-has-a-different-chip-under-the-hood); [9to5Google specs](https://9to5google.com/2024/08/06/google-tv-streamer-specs-memory-storage-chip/); TechRadar: its chip "isn't really up to" Apple TV 4K competition — [TechRadar](https://www.techradar.com/televisions/streaming-devices/dont-expect-the-google-tv-streamer-to-be-a-true-apple-tv-4k-competitor-its-chip-isnt-really-up-to-that)
- Android Central: Shield TV Pro "has much better hardware" and handles Kodi without trouble; Google TV Streamer also runs Kodi; Shield has 2x USB-A, Streamer one USB-C — [Android Central comparison](https://www.androidcentral.com/streaming-tv/google-tv-streamer-vs-nvidia-shield-tv-pro-2019); XDA: Tegra X1+ with 256-core GPU is "in a different performance class altogether" from Onn 4K Pro and Streamer, though the $60 Onn 4K Pro covers most Kodi/local-file use — [XDA on Onn 4K Pro](https://www.xda-developers.com/the-60-onn-4k-pro-does-all-the-things-i-kept-my-nvidia-shield-around-for/)
- Jellyfin Android TV 0.20 beta adds "Allow Shield Dolby Vision Profile 7 direct playback" and HDR force-enable/disable preferences; Jellyfin codec matrix: Android TV HEVC 10-bit "conditional" and AV1 "conditional (needs Android TV 10)" because it depends on the device — [jellyfin-androidtv v0.20.0-beta.1](https://github.com/jellyfin/jellyfin-androidtv/releases/tag/v0.20.0-beta.1); [Jellyfin codec support](https://jellyfin.org/docs/general/clients/codec-support/)
- libass CPU cost signals: Media3 PR notes `draw_ass_rgba` "is too slow for 4K bitmaps" without NEON/OpenMP — [androidx/media PR #2324](https://github.com/androidx/media/pull/2324); Kodi/CoreELEC user saw ~2 min CPU per ASS episode with ~4,000 fonts in the font folder — [CoreELEC forum](https://discourse.coreelec.org/t/external-ass-and-external-fonts-performance/15715)

### Inferences
- For a libass-heavy anime library: Shield TV Pro gives the most CPU/GPU headroom for 4K ASS compositing and is the only one with lossless passthrough and DV P7 direct play in Jellyfin 0.20 beta, but likely lacks AV1 hardware decode (treat JellyWatch's ✓ as suspect) and lacks HDR10+. Onn 4K Pro is the best value with AV1/HDR10+/DV but no TrueHD/DTS passthrough and weaker CPU. Google TV Streamer and Fire TV Stick 4K Max share the MT8696 family (AV1 ✓, no lossless passthrough); Fire TV's DV is reported unreliable for local playback.
- Since anime is mostly 1080p, 4K-bitmap libass cost is less relevant than CPU single-thread speed for complex typesetting; keep system/user font folders small on all devices.

### Gaps
- No benchmark of libass render cost on any of the four devices was found.
- Shield AV1 hardware decode: sources conflict; Nvidia's own spec page was not fetched.
- Google TV Streamer 10-bit HEVC and passthrough specifics: not explicitly confirmed by any fetched source (only codec names listed).
- Onn 4K Pro / Google TV Streamer behaviour with Jellyfin 0.20's libass path has not been reported in any source.

## Key Question 7: Desktop (Windows/Linux/macOS) front-ends with libass

### Takeaway
On desktop, mpv-based front-ends (Jellyfin Media Player, Jellyfin MPV Shim, Plex MPV Shim) and Kodi give full libass rendering with per-user server logins; this is uncontroversial and well-established.

### Cited Findings
- Jellyfin MPV Shim: libmpv-based (loads `mpv-2.dll` on Windows); Windows binaries, Linux via Flathub/pip, macOS via Homebrew/pipx (only external MPV backend works on macOS); login by server URL + credentials or Quick Connect; casting from Jellyfin apps; "Fast User Switching" gives each non-default user its own Jellyfin device ID; SyncPlay supported (noted fragile); 2.5k stars — [jellyfin/jellyfin-mpv-shim](https://github.com/jellyfin/jellyfin-mpv-shim)
- Plex MPV Shim exists on PyPI (v1.7.3) for casting from Plex apps to mpv — [plex-mpv-shim on PyPI](https://pypi.org/project/plex-mpv-shim/1.7.3)
- Jellyfin Media Player listed as supporting HEVC 8/10-bit, H.264 10-bit, AV1, VP9 — [Jellyfin codec support](https://jellyfin.org/docs/general/clients/codec-support/); 2026 guide: ASS rendering needs "a client that supports it, such as Jellyfin Media Player or mpv-based clients" — [JellyWatch subtitles guide](https://jellywatch.app/blog/jellyfin-subtitles-complete-guide-2026)
- MPC-HC has an open request to add a libass option for ASS rendering — [clsid2/mpc-hc #205](https://github.com/clsid2/mpc-hc/issues/205)

### Inferences
- Desktop is a solved problem: Kodi, JMP, or MPV Shim all use libass natively and respect MKV font attachments through mpv/Kodi; the only decision is UI preference.

### Gaps
- Plex HTPC / Plex for Windows ASS behaviour not researched.
- Emby Theater desktop not researched.
