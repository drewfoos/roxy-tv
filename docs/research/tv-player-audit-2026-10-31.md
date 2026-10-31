# Roxy TV player audit (2026-10-31)

Scope: the libmpv player as the Android TV uses it: `PlayerActivity` hosting `PlayerSession`,
`MpvPlayer`, `PlayerControls`, `AudioPassthrough` (app 0.4.6, `dev.jdtech.mpv:libmpv:1.0.0`).
Verified facts about the build: the AAR is **mpv v0.41.0** and contains the `hwdec_aimagereader`
interop, `mediacodec_embed`, libplacebo (`gpu-next`) and dav1d.

Target device facts (read from the TCL over adb, read-only): MediaTek MT9689, 2.4 GB RAM with
about 200 MB free, panel **3840x2160 at 60 Hz only** (no 24/50 Hz modes), HDR types 1-4
(Dolby Vision, HDR10, HLG, HDR10+). The UI and the SurfaceView render at 1920x1080.

Severity: **P1** = user-visible bug, **P2** = robustness or performance problem, **P3** = polish.

## Status: fixed in app 0.5.0 (same day)

Every item below is addressed in 0.5.0 except P3-1, which stays a note.

Verified on the TCL with the perf build against pookiebox:
- `hwdec=mediacodec,mediacodec-copy` picks zero-copy (`aimagereader`); colours correct; 1080p
  H.264 at 0 dropped frames and about 26 % of one core (37 % with copy mode).
- Left/Right scrub with the trickplay tile and one seek when the presses stop (P1-6).
- Explicit Pause on a paused video stays paused (P1-4).
- Home has focus on "Continue" after leaving the player (P3-4).
- mpv is destroyed on a background thread (P2-2).
- A deep link for the item already playing, sent while the player sat behind Home, resumes it
  with its picture (P1-2 same-item path, P1-3).
- The media token goes in an `X-Roxy-Media-Token` header; against a server without header
  support the player falls back to `?mt=` after one refused open (about 85 ms).

Correction to P2-6: lowering `msg-level` does not stop the duplicate `V/mpv` lines. The
libmpv binding prints every log message it receives to logcat itself, at the level it
requested. The token fix is the header above, with the server change in `server/internal/auth`.
Deployed to pookiebox the same day: the TCL opens streams with the header on the first try
and the token no longer appears in logcat.

Not exercised on a device: reconnect after a server drop (P1-1), a new play request for a
different title (P1-2), native HDR and smooth motion (opt-in settings), the media session
from Assistant, refresh-rate switching (the TCL only has 60 Hz), and the phone host.

## Bugs

### P1-1 A dropped connection can count as "finished" and skip to the next episode
`PlayerSession` treats `eof-reached` as the natural end: it reports "end" and, with autoplay,
calls `playNext()`, which also marks the episode watched. With `keep-open=always`, an HTTP stream
that ends early (server restart, Wi-Fi drop, container redeploy) also reaches EOF. `onEndFile`
ignores the end-file reason, so errors look the same as a real end.
Fix: on `eof-reached`, treat it as finished only when `time-pos >= duration - 10 s` (or the
source's known duration). Otherwise reload the same URL at the last position, with a small
retry/backoff and an OSD "Reconnecting…". Read `MPV_EVENT_END_FILE`'s reason (eof / error /
stop / redirect) where the binding exposes it; otherwise use the position check.
Also add ffmpeg reconnect options for HTTP (see the research section).
Verify: kill the server mid-episode on the WSL dev box and watch what the TV does.

### P1-2 A new play request is ignored while the player is open in the background
`PlayerActivity` is `launchMode="singleTask"` and has no `onNewIntent`. Pressing Home during
playback stops the activity but keeps it. Starting another title from the Watch Next row
(`roxytv://play/<id>` → `MainActivity` → `playItem` → `startActivity`), from "Play on", or from the
app itself brings the old instance back with the **old** episode.
Fix: override `onNewIntent`, rebuild `Params`, and either `session.destroy()` + new session or
add a `PlayerSession.load(req)` path shared with `playNext()`. Alternatively drop `singleTask`.

### P1-3 Black picture after a load that happens while the surface is gone
`surfaceDestroyed` sets `vo=null`. `surfaceCreated` only restores `vo` when there is **no** pending
URL: `if (pendingUrl != null) loadPending() else setPropertyString("vo", options.vo)`.
If a load is queued while the surface is away (the passthrough watchdog's reload, a quality
change, the next episode from "Play on", a transcode restart), the file loads with `vo=null`:
audio, no picture. Fix: always set `vo` back in `surfaceCreated`, then load the pending file.

### P1-4 Media keys: Play pauses and Pause plays
`onKeyDown` maps `KEYCODE_MEDIA_PLAY`, `KEYCODE_MEDIA_PAUSE` and `PLAY_PAUSE` all to
`togglePause()`. HDMI-CEC remotes, Bluetooth remotes and Assistant send explicit PLAY / PAUSE, so
"pause" on an already-paused video starts it. Map PLAY to `setPaused(false)` and PAUSE to
`setPaused(true)`. Also add `MEDIA_NEXT` (next episode), `MEDIA_STOP` (stop + back) and
`MEDIA_PREVIOUS` (restart, or previous episode when near the start).

### P1-5 The passthrough watchdog blames the soundbar when the viewer pauses
`watchPassthrough` counts `pause == true` as "waiting" for only 4 checks (about 21 s). A viewer who
pauses in the first seconds and waits longer trips it: the codec is added to `brokenSpdif`,
**saved to SharedPreferences for good**, and the file reloads. From then on that TV decodes AC3
locally until someone finds "reset passthrough". Fix: only count time while `pause == false` and
`paused-for-cache == false`; restart the 6 s window on unpause. Key the remembered failure by the
HDMI output (device name / EDID encodings) so a new soundbar starts clean.
Background from the research: mpv's `ao_audiotrack` sends every passthrough format as
`ENCODING_IEC61937` on a stereo channel mask at the output's native rate. E-AC3 needs a 192 kHz
carrier and TrueHD/DTS-HD MA need 8-channel high-bit-rate output, so only AC3 and DTS core are
reliable. That likely explains the E-AC3 black screen on the TCL. The TCL now detects
`[ac3, dts]`, which is the right list. Do not offer TrueHD or DTS-HD passthrough.

### P2-1 End-of-file runs the progress/Watch Next logic twice and as a "pause"
With `keep-open=always`, mpv's default `keep-open-pause=yes` pauses at the end. That fires
`onPauseChanged(true)` → `reportProgress("pause")` → `WatchNext.publish`, next to the two
`reportProgress("end")` calls from `onEndFile` and `onEofReached`. Publish removes entries past
97 %, so this mostly works out, but it is three network calls and a Watch Next write racing the
next episode's `markWatched`. Ignore the pause that comes with `eof-reached`, and report "end" once.

### P2-2 `MPVLib.destroy()` runs on the main thread
`mpv_terminate_destroy` waits for the demuxer and decoder threads. A stalled HTTP read (server
gone) can hold it for the network timeout, which means an ANR on Back. Detach the surface, set
`vo=null`, stop, then destroy on a background thread. Every property read already checks
`destroyed` first; keep it that way.

### P2-3 Event-thread callbacks touch app state off the main thread
`onFileLoaded`, `onEndFile` and `onEofReached` run on libmpv's event thread and call
`reportProgress`, which reads Compose state (`ui.paused`) and writes `lastReportedMs` (not
volatile), and calls `app.remote.reportState`. `onPauseChanged` posts to main, the others do not.
Post every listener call to the main thread, as `onPauseChanged` already does. This also removes
the window where a callback reads mpv properties while `destroy()` frees the handle.

### P2-4 `forceSid` survives into the next episode
`Options.forceSid` (the adb/diagnostics override) is applied on every `FILE_LOADED`, so the next
episode gets the same mpv track number even when it is a different track. Clear it after the
first file. Related: the viewer's own mid-episode pick (`lastSid`) is not carried to the next
episode either. A "remember my pick for this show" rule (match by language + title class, not by
id) is what Crunchyroll and Plex do.

## Performance (the TCL is the constraint)

### P2-5 Hardware decoding copies every frame back to the CPU
The default is `hwdec=mediacodec-copy`. The bundled mpv 0.41 has the AImageReader interop, so
`hwdec=mediacodec` with `vo=gpu`/`gpu-next` is **zero-copy and still renders libass subtitles**.
On a 2.4 GB 32-bit MediaTek TV, copying 4K HEVC 10-bit frames (about 24 MB each) is the most
likely cause of drops on 4K files. Recommendation: default to `mediacodec,mediacodec-copy`
(mpv tries them in order), keep the copy mode as the manual fallback, and update the TV hint text.
Two more reasons. `mediacodec-copy` cannot represent 10-bit output on many SoCs, which gives green
or garbled HEVC 10-bit frames on Amlogic and Fire TV (mpv-android 462, 540, 1088, 1206). And the
TCL's Mali-G52 logs "16 bit UNORM textures not available", so copied 10-bit frames need an extra
conversion. `hwdec=auto` would not help: since mpv 0.40 it means `auto-safe`, which never picks
the zero-copy `mediacodec`.
Measured on the TCL with the current default: 1080p H.264 plays with zero dropped frames at about
37 % of one core. Measure a 4K HEVC 10-bit file with both modes before switching the default.

### P2-6 mpv logs everything at verbose level, and release builds pay for it
`msg-level=all=v` sends every verbose line through JNI into `Timber.v`. `RoxyApp` plants
`Timber.DebugTree()` in **every** build type, and `DebugTree` creates a `Throwable` per call to
find the tag. Around loads and seeks that is hundreds of stack walks per second on the TCL, and
it is why the player floods logcat. Fix: plant `DebugTree` only in debug, use `msg-level=all=warn`
by default, and raise it to `v` only while the stats overlay is open (`msg-level` is a runtime
property). Keep the libass/ao lines the stats panel shows by filtering at `info` for those
modules.

Seen on the TCL: **every mpv line is logged twice**, once by libmpv itself (tag `mpv`) and once by
our observer. mpv's own lines also print the **full stream URL with the `mt=` media token**, which
our code deliberately strips. Lowering `msg-level` stops both.

### P2-7 The controls poll mpv twice a second, even when hidden
`refreshUi()` runs every 500 ms and calls `tracks()`: about 7 JNI reads per track plus about
15 more, then builds a new `PlayerUiState`, so the whole controls tree recomposes twice a second.
Every other second `snapshot()` reads about 30 more properties for the stats overlay even when
it is off. Meanwhile `time-pos` and `duration` are **observed** but their callbacks are empty.
Fix: observe what the UI needs (`pause`, `paused-for-cache`, `seeking`, `track-list`,
`current-tracks/sub/id`, `current-tracks/audio/id`, `duration`, and `time-pos` as
`MPV_FORMAT_INT64`, which mpv only re-sends when the whole second changes) and feed state from
the events. Rebuild the track list only on `track-list` changes. Skip `snapshot()` unless
`showStats`. Split position into its own state so only the seek bar and clock recompose.

### P2-12 Two settings offer known-broken choices
- **`vo=gpu-next`**: the bundled libplacebo 7.360.1 lacks the GL sync-object leak fix. After about
  15-20 minutes the picture turns purple or freezes while audio continues (mpv-android 977,
  Findroid 686). It also lacks the AV1 film-grain fix on GLES (Findroid 1322). Hide `gpu-next`
  until libmpv ships libplacebo 7.387.0 or later, or force `vd-lavc-film-grain=cpu` with it.
- **`ao=aaudio`** (an `Options` value): with libmpv 1.0.0, video freezes after a seek or audio-track
  switch while audio continues (Findroid 1246). Keep `audiotrack` and set `ao=audiotrack,opensles`
  so there is a fallback.

### P3-1 4K panel, 1080p surface
The SurfaceView buffer is 1920x1080, so mpv downscales 4K video and the TV upscales it again.
That is the right call for this GPU. Note it in docs; a `setFixedSize(3840, 2160)` path is only
worth trying on a stronger box (Shield, Chromecast 4K) behind a setting.

## Missing features that make a player feel finished

### P2-8 No buffering indicator
Nothing shows `paused-for-cache` or `seeking`; a stall looks like a frozen picture. Add a
spinner (after a 300-500 ms grace period) and optionally the cache percentage
(`cache-buffering-state`).

### P1-6 Left/Right seek once, then walk the button row (confirmed on the TCL)
With the controls hidden, Right seeks +10 s and opens the controls with focus on **Pause**. Every
further Right moves focus along the row (+10, volume, CC, speed, gear) instead of seeking. On the
TCL, eight quick Right presses gave one 10 s seek and left focus on the settings gear. YouTube and
Netflix TV keep Left/Right on the timeline while the controls are up.
Fix: open the controls with focus on the seek bar, with Down reaching the buttons. On the bar,
scrub a preview position with the trickplay tile, accelerate with `repeatCount` (10 s, then 30 s,
then 60 s), and commit one absolute seek on key up or after about 600 ms idle. Even at 10 s a step,
crossing a 23-minute episode takes about 140 presses today. mpv merges queued seeks, so this is
about feel, not load. Relative seeks land on keyframes by default (`hr-seek=default`), so +10 can
move 7 or 13 s; seek to the absolute target instead.

### P2-10 No audio focus and no MediaSession
mpv's `audiotrack` output does not request audio focus, and the app has no `MediaSession`.
Effects on the TV: Assistant "pause"/"resume", the system "now playing" surface, and some CEC
paths do not reach the player; on the phone, other apps keep playing over it and unplugging
headphones (`ACTION_AUDIO_BECOMING_NOISY`) does not pause. A small `MediaSessionCompat` (or a
Media3 `SimpleBasePlayer` adapter, as Findroid does) fixes all of these at once.

### P2-11 No frame-rate hint
Nothing calls `Surface.setFrameRate` or sets `preferredDisplayModeId`. The TCL only has 60 Hz,
so 23.976 fps anime gets 3:2 pulldown judder there whatever we do, but Google TV boxes and better
TVs can switch to 24 Hz. Add `setFrameRate(container-fps, FRAME_RATE_COMPATIBILITY_FIXED_SOURCE)`
on file load (API 30+) and re-read `display-fps` after the switch; `display-fps-override` is
currently fixed at session start. For 60 Hz-only panels, an optional "Smooth motion" setting
(`interpolation=yes`, `video-sync=display-resample`, `tscale=oversample`) removes the judder at a
GPU cost; test it on the TCL before offering it there.

### P3-2 HDR plays as tone-mapped SDR
The panel supports HDR10/HLG/DV, but `vo=gpu` on Android draws an SDR surface, so HDR files are
tone-mapped and look dimmer than in the TV's own apps. Options are in the research section; the
honest short-term answer is a per-file "HDR passthrough" mode using `vo=mediacodec_embed` (the
decoder draws straight to the surface and the TV does HDR) at the cost of libass subtitles.

### P3-3 Smaller items
- `tls-verify=no` is set globally. It is harmless on LAN HTTP but disables certificate checks on a
  future HTTPS/Tailscale-cert setup. Turn it back on (`ca-file` from the Android store if needed).
- `PlayerActivity` is `exported="true"` in the main manifest, so any app can start playback of an
  arbitrary URL with an arbitrary `itemId` (and progress reports for it). Export it only in the
  debug manifest, which is what the adb diagnostics need.
- `cachedMediaToken` returns a token without checking its expiry; a 24 h+ session would then fail
  on its next seek.
- Auto-skip remembers skipped segments by type, so a second segment of the same type is not skipped.
- `demuxer-max-bytes=64MiB` + 32 MiB back buffer: mpv-android uses 64/64 (32 below API 27). On the
  TCL with 200 MB free, use 32/16 MiB when total RAM is under 3 GB.
- mpv already enables ffmpeg's HTTP `reconnect=1` (`reconnect_delay_max=7`). Adding
  `stream-lavf-o=reconnect_on_network_error=1` covers more drop types; P1-1 still needs the
  position check for a server that stays down longer than that.
- `idle=yes` could be `idle=once`, matching mpv-android; not a bug.

### P3-4 Focus is lost after leaving the player
Seen on the TCL: Back from the player returns to Home with no focus shown on the hero buttons.
Restore focus to the button that started playback.

## What other mpv-on-Android players do

Sources were read at source level: mpv v0.41.0 tag, mpv-android master (2026-10-30), Findroid
main, the libmpv-android JNI, Aniyomi, mpvKt, mpvEx.

**The AAR** (`dev.jdtech.mpv:libmpv:1.0.0`, 2026-04-08): mpv 0.41.0, FFmpeg 8.1, libplacebo
7.360.1, libass 0.17.4, dav1d 1.5.3, fontconfig 2.17.1, no patches. Confirmed in the TCL's log.

**mpv-android** (the reference):
- Same surface pattern as Roxy: attach, `force-window=yes`, then load or set `vo`; on destroy
  `vo=null`, `force-window=no`, detach.
- `hwdec=mediacodec,mediacodec-copy`, `ao=audiotrack,opensles`, `profile=fast`, explicit
  `display-fps-override`, `tls-verify=yes` with a bundled CA file.
- Observes `time-pos` as INT64 (one update per second) and `duration/full` as DOUBLE.
- Audio focus: pause on loss, duck on `CAN_DUCK`, re-requested on every unpause. A
  `MediaSessionCompat` maps play/pause/seek/next/prev.
- Does **not** do refresh-rate switching (issue 12, open since 2016), HDR passthrough, or a
  passthrough UI (spdif stall issue 619 still open).

**Findroid** (wraps libmpv as a Media3 `Player`):
- Posts every callback to the main looper. Maps `paused-for-cache` and `MPV_EVENT_SEEK` to
  BUFFERING, `PLAYBACK_RESTART` to READY, `eof-reached` to ENDED.
- Observes `track-list` as a STRING (JSON) instead of reading it property by property.
- Whole-second positions plus whole-second seeks once caused an endless skip-intro loop; it now
  seeks in fractions. Roxy already seeks with `%.3f`.
- An early ANR on leaving the player was fixed by removing observers before `destroy()`. Roxy
  already does this.

**Aniyomi / mpvKt / mpvEx** (anime-focused):
- **AniSkip**: fetches opening/ending times by MAL id + episode + duration. A good extra segment
  source for Roxy Server when a file has no chapters.
- An optional `sub-ass-override=force` toggle for badly styled releases, off by default. Worth a
  per-show toggle; keep `no` as the default for typeset anime.
- mpvEx has a precise-seek toggle and ships Anime4K shaders. Anime4K is not realistic on the TCL.
- Optional `deband=yes` helps banding in 8-bit anime encodes; measure the GPU cost first.
- None of them is TV-focused; their D-pad support requests are open.

**mpv 0.41 manual points that matter here**
- Buffering: observe `paused-for-cache`, `seeking`, `core-idle` (FLAG) and `cache-buffering-state`
  (INT64). Show a spinner when `paused-for-cache || seeking || (core-idle && !pause)`.
- The libmpv-android JNI only delivers event ids: no `end-file` reason, and NODE properties are
  dropped. Observe those as STRING (JSON). `screenshot-raw` is not usable.
- Callbacks run on the native event thread while `MPVLib` holds a lock. Never block in them or wait
  on the main thread from them, or `destroy()` on main deadlocks. `client.h` suggests the `quit`
  command plus waiting for `MPV_EVENT_SHUTDOWN` for asynchronous teardown.
- Refresh-rate matching is the app's job: `Surface.setFrameRate(fps,
  FRAME_RATE_COMPATIBILITY_FIXED_SOURCE, CHANGE_FRAME_RATE_ALWAYS)` with the exact rate, wait for
  the display change, then update `display-fps-override`.
- HDR: the Android EGL context has no colour-space hook in 0.41, so `target-colorspace-hint` does
  nothing and HDR is always tone-mapped (mpv-android 588). An unmerged mpv PR (18710) adds it.
- New in 0.41: `sub-glyph-limit` and `sub-bitmap-max-size` (memory caps for heavy typesetting on
  low-RAM TVs), `hwdec-threads`. Renamed in 0.40: `hwdec-software-fallback`, `sub-hinting`,
  `sub-shaper`.
- An AImageReader fix for zero-copy on some devices (mpv PR 18453) landed after 0.41 and is **not**
  in the AAR. If `hwdec=mediacodec` shows frozen or corrupt video on a device, that is the likely
  cause; keep the copy fallback.

## Suggested order of work

1. Quick fixes: P1-3, P1-4, P1-5, P2-4, P2-12, and debug-only Timber plus a lower `msg-level` (P2-6).
2. TV feel: P1-6 (seek bar focus and scrubbing), P2-8 (spinner), P3-4 (focus on return).
3. Robustness: P1-1 (early EOF), P1-2 (`onNewIntent`), P2-1 and P2-3 (event threading), P2-2.
4. Performance: P2-7 (event-driven state), then measure P2-5 on a 4K HEVC 10-bit file on the TCL.
5. Platform: P2-10 (MediaSession and audio focus), P2-11 (frame-rate hint, optional smooth motion).
6. Later: a libmpv build with libplacebo 7.387+ and mpv PR 18453, then revisit `gpu-next` and HDR.
