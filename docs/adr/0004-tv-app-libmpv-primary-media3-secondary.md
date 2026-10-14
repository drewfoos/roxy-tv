# ADR-0004: Roxy TV app uses libmpv as the primary engine, Media3 as secondary

Status: accepted, 2026-10-13

## Context

Google declared the libass native decoder for Media3 "unlikely to merge" (androidx/media
PR #2324). The de-facto Android libass path is the third-party `ass-media` overlay
(libass-android), which has frame-timing alignment caveats, needed glyph-cache tuning, and
does not document MKV font-attachment parsing. The official Jellyfin Android TV app pulled
it from the 0.19 release and reintroduced it in the 0.20 betas disabled by default.

libmpv brings FFmpeg software decoders (Hi10P) and libass (with native MKV attachment
reading) in one engine; it is what Jellyfin Desktop, Streamyfin, Findroid and Kodi-adjacent
players rely on. Its costs are a 20-40 MB native payload and owning hwdec, HDR and
passthrough quirks per SoC. Wholphin and Findroid both converged on dual engines.

## Decision

- `PlayerEngine` interface with two implementations.
- **libmpv** is the default for any file with ASS/SSA tracks or Hi10P video, and the
  fallback for anything the Media3 path fails on. `hwdec=mediacodec-copy`, software
  decode fallback, `sub-fonts-dir` pointed at the app font cache.
- **Media3/ExoPlayer** with `ass-media` (OpenGL mode, raised glyph cache, explicit font
  download) for plain hardware-decodable files, HDR/Dolby Vision passthrough and tunneled
  playback.
- Engine choice is a rule set, overridable per device and per playback.

## Consequences

- Kodi-grade ASS fidelity on day one via libmpv; Media3 keeps Google's HDR/audio behaviour
  where it matters.
- The app must be tested on the actual TV SoC; hwdec behaviour differs per vendor.
- APK size grows by the mpv payload; acceptable for a TV app.
