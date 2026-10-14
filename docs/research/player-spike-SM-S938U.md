# Player spike results: Samsung Galaxy S25 Ultra (SM-S938U)

Date: 2026-10-13 (set 1, five files) and 2026-10-14 (set 2, five more: H.264 8-bit,
HEVC 8-bit 60 fps with 5.1 audio, Blu-ray shape with TrueHD + PGS, HLG, MP4 + mov_text). Device: Samsung SM-S938U, Qualcomm SM8750 (Snapdragon 8 Elite, Adreno
830), 8 cores, Android 17 (API 37), display 1440x3120 up to 120 Hz, HDR10 / HLG / HDR10+.
Player: libmpv 0.41.0 (dev.jdtech.mpv:libmpv 1.0.0, FFmpeg n8.1, libplacebo 7.360.1),
libass 0.17.4, HarfBuzz 14.1.0. Server: Roxy Server dev build on the laptop over Wi-Fi.

**Scope.** This phone is far faster than the living-room TV, so this run validates
*correctness* (libass rendering, font selection, hwdec selection, HDR signalling, the
native API end to end). The performance decision (default `hwdec`/`vo`, whether 4K ASS
needs server burn-in) still has to come from the Costco TV. Raw output (overlay
screenshots, mpv logs, `results.csv`): `apps/tv/spike-runs/20261013-SM-S938U/`.

## Matrix (vo=gpu, ~12 s into each file, 23.976 fps content)

CPU is mpv's own process, as "% of one core". Drops were 0 (vo / decoder / delayed /
mistimed) in every run.

| File | hwdec requested | decoder actually used | output format | CPU |
|---|---|---|---|---|
| Hi10P x264 1080p | mediacodec-copy | software (MediaCodec refuses High 10) | yuv420p10 | 45% |
| Hi10P x264 1080p | mediacodec | software (same) | yuv420p10 | 47% |
| Hi10P x264 1080p | no | software | yuv420p10 | 33% |
| HEVC Main10 1080p | mediacodec-copy | mediacodec-copy | nv12 | 17% |
| HEVC Main10 1080p | mediacodec | mediacodec (surface) | mediacodec | 13% |
| HEVC Main10 1080p | no | software | yuv420p10 | 54% |
| HEVC Main10 4K HDR10 | mediacodec-copy | mediacodec-copy | nv12 | 28% |
| HEVC Main10 4K HDR10 | mediacodec | mediacodec (surface) | mediacodec | 16% |
| HEVC Main10 4K HDR10 | no | software | yuv420p10 | 99% |
| Dual audio (HEVC10, FLAC + E-AC-3) | mediacodec-copy | mediacodec-copy | nv12 | 16% |
| Dual audio | mediacodec | mediacodec | mediacodec | 16% |
| Dual audio | no | software | yuv420p10 | 41% |
| AV1 10-bit 1080p | mediacodec-copy | mediacodec-copy | nv12 | 18% |
| AV1 10-bit 1080p | mediacodec | mediacodec | mediacodec | 16% |
| AV1 10-bit 1080p | no | software | yuv420p10 | 51% |

Set 2 (2026-10-14, same device and build; raw output in `apps/tv/spike-runs/20261014-SM-S938U-set2/`):

| File | hwdec requested | decoder actually used | output format | CPU | drops |
|---|---|---|---|---|---|
| H.264 8-bit 1080p | mediacodec-copy | mediacodec-copy | nv12 | 14% | 0 |
| H.264 8-bit 1080p | mediacodec | mediacodec (surface) | mediacodec | 11% | 0 |
| H.264 8-bit 1080p | no | software | yuv420p | 39% | 0 |
| HEVC 8-bit 1080p **60 fps**, FLAC 5.1 | mediacodec-copy | mediacodec-copy | nv12 | 35% | 0 |
| HEVC 8-bit 1080p 60 fps, FLAC 5.1 | mediacodec | mediacodec (surface) | mediacodec | 26% | 0 |
| HEVC 8-bit 1080p 60 fps, FLAC 5.1 | no | software | yuv420p | 78% | vo=3 |
| Blu-ray shape: H.264, **TrueHD 5.1**, **PGS** | mediacodec-copy | mediacodec-copy | nv12 | 15% | 0 |
| Blu-ray shape | mediacodec | mediacodec (surface) | mediacodec | 14% | 0 |
| Blu-ray shape | no | software | yuv420p | 42% | 0 |
| HEVC Main10 1080p **HLG** | mediacodec-copy | mediacodec-copy | nv12 | 18% | 0 |
| HEVC Main10 1080p HLG | mediacodec | mediacodec (surface) | mediacodec | 14% | 0 |
| HEVC Main10 1080p HLG | no | software | yuv420p10 | 53% | 0 |
| H.264 **MP4**, AAC stereo, mov_text | mediacodec-copy | mediacodec-copy | nv12 | 11% | 0 |
| H.264 MP4 | mediacodec | mediacodec (surface) | mediacodec | 9% | 0 |
| H.264 MP4 | no | software | yuv420p | 37% | 0 |

## Findings

1. **libass renders the test ASS exactly as authored.** Styled dialogue, positioned and
   fading signs, the moving rotated sign, karaoke fill (orange to white), `\fscx300`
   stretch, blur, BorderStyle 3 box, vector drawing, CJK text. Verified visually on the
   screenshots for all 15 runs.
2. **MKV-attached fonts are used.** `fontselect: (Oswald, 700, 0) -> Oswald-Regular` and
   `(Noto Sans CJK JP, 400, 0) -> NotoSansCJKjp-Regular` in every run, with
   `embeddedfonts=yes`. Oswald is a font no device ships, so this proves attachment
   loading, not a lucky system match.
3. **libass falls back to system fonts for glyphs the attachment lacks.** The `→` glyph
   (U+2192) missing from Oswald resolved to `/system/fonts/SECFallback-Bold.ttf`, and the
   bold weight of Noto Sans CJK JP resolved to the phone's `/system/fonts/NotoSansCJK-Regular.ttc`.
   A Google TV box will not have Samsung's fallback set and may lack a CJK font entirely,
   so the app's bundled Noto CJK fallback (`sub-fonts-dir`) stays mandatory.
4. **Hi10P is software-only, as expected everywhere.** `h264_mediacodec` fails with
   "Could not open codec" on High 10 and mpv falls back to software automatically with no
   visible hiccup. On this SoC 1080p Hi10P plus libass costs about a third of one core.
5. **HEVC Main10, 4K HDR10 and AV1 10-bit all decode in hardware** on this SoC in both
   `mediacodec-copy` and direct `mediacodec` modes. Direct `mediacodec` is the cheapest
   (13 to 16% of a core), `mediacodec-copy` costs roughly 1.3 to 1.8x that, software
   costs 3 to 6x and nearly saturates a core at 4K.
6. **`mediacodec-copy` delivers 8-bit `nv12` for 10-bit content.** The copy path loses
   bit depth; direct `mediacodec` keeps the decoder's native surface format. For HDR and
   10-bit SDR anime this favours direct `mediacodec` (or software) over copy mode.
7. **HDR10 is signalled correctly.** The 4K file reports `bt.2020-ncl / pq / bt.2020`
   through mpv and the output stage. The phone's screenshots show tone-mapped colours;
   the software path and the hardware paths render visibly different hues for the same
   frame, so HDR output handling needs a real check on the TV (which can display PQ
   natively) rather than on a phone screenshot.
8. **Track selection needs explicit policy.** With `sid=auto` mpv picked no subtitle
   track on a file whose ASS track has no default flag. Setting `slang=eng,en` fixed it;
   the real app must drive `slang`/`alang` (and forced-sub handling) from the user profile.
9. **PGS bitmap subtitles render correctly** (set 2). mpv's `pgssub` decoder drew the
   synthetic Blu-ray cues at the right position with the palette and alpha intact, and
   cue 3 replaced cue 2 on time with no ghosting. Nothing in the player needed changing;
   `slang=eng` selected the PGS track like any other.
10. **TrueHD 5.1 and FLAC 5.1 decode** (set 2): the overlay shows `truehd s32 ch=6` and
    `flac s32 ch=6` through `ao=audiotrack`. The phone downmixes to its speakers, so
    channel mapping and HDMI passthrough (TrueHD, E-AC-3, AC-3 bitstreaming) are still
    untested and need the TV plus a receiver.
11. **60 fps HEVC holds in hardware** (set 2): 0 drops at 26% of a core with direct
    `mediacodec`. Software decode at 60 fps was the only configuration in 30 runs that
    dropped frames (3 vo drops at 78% of a core), which is the first hint of where a
    weaker SoC will fall over first.
12. **HLG is signalled correctly** (`bt.2020-ncl / hlg / bt.2020`) and decodes in
    hardware like HDR10. Same caveat as HDR10: output correctness needs the TV.
13. **MP4 + mov_text works** (set 2): mov_text is converted to ASS by mpv and rendered by
    libass with the system sans-serif (Roboto on this phone). On a TV without a decent
    system font the bundled fallback font covers this too.
14. **The native API path works end to end.** Login (`POST /v1/auth/login`), item list
   with sources, and direct play from `/v1/stream/{sourceId}?api_key=` all worked from the
   phone over Wi-Fi; range requests served by Roxy Server at ~3.6 MB/s cache fill.

## Real releases (2026-10-14)

Two real files from the owner's collection, copied into `testdata/out` (git-ignored):

| File | Shape | Result |
|---|---|---|
| KonoSuba: Legend of Crimson (EMBER, 1.96 GB) | HEVC Main10 1080p, AC-3 5.1 jpn + AC-3 2.0 jpn + AAC 2.0 eng, 5 subtitle tracks (4 ASS, 1 SRT), one attached font (arial.ttf), cover art | hw decode in all three modes (10 to 12% of a core direct `mediacodec`), 0 drops through +10 s seeks, a jump to 13 min and seeks back; cache refilled at ~350 to 450 KiB/s over Wi-Fi |
| Toy Story 5 (WeSLeY, 5.8 GB) | HEVC Main10 1920x1038, E-AC-3 5.1 Atmos, 3 SRT tracks | hw decode, 0 drops through the same seek pattern, 9 to 12% of a core |

Findings that the synthetic set could not produce:

15. **Subtitle track titles and flags are not enough to pick a track.** KonoSuba carries
    two tracks both titled "Dialogue@CR" and both tagged `eng`; the first is actually
    Spanish. The release's *default* flag points at "Signs@EMBER", which is a signs-and-
    songs track (no dialogue at all), as dual-audio releases do for dub viewers. With
    `slang=eng` mpv picked the signs track, and a title-based "prefer dialogue" rule then
    picked the Spanish one. Fix, in two parts:
    - **Roxy Server sniffs the language of every text subtitle track at ingest**
      (`internal/langdetect`, stop-word + script classifier) and exposes it as
      `detectedLanguage` next to the container tag. It flagged the mislabelled track on
      the first scan (`tagged=eng detected=spa`).
    - **The player chooses by (detected language, title class, codec)** instead of the
      default flag: "full" mode scores signs/songs titles down and dialogue titles up;
      ASS beats SRT; the server's detected language overrides the tag. With that the
      English ASS dialogue track was selected and rendered (screenshots in
      `apps/tv/spike-runs/20261014-konosuba-dialogue3/`).
16. **Audio defaults to the English dub** when present (`alang=eng,en,jpn,ja`), per the
    owner's preference. The companion subtitle choice (full dialogue vs signs only) must
    come from the user's profile, not from the file's default flag.
17. **Fonts in real releases are the weak spot, now covered by a bundled fallback set.**
    The English track asks for "Open Sans Semibold", which is not attached (only
    arial.ttf is); before the bundle libass fell back to the phone's Roboto Bold. The app
    now ships 50 fallback fonts (`apps/tv/FONTS.md`, fetched by `tools/fetch-fonts.ps1`):
    Open Sans in six styles, Liberation Sans/Serif, Carlito, Lato, Montserrat, Source
    Sans 3, Fira Sans, Cabin, Gandhi Sans, Noto Sans, DejaVu Sans, Noto Sans CJK JP, and
    Noto Sans Symbols / Symbols 2 / Math / Music / Emoji. With it the same track resolves
    to `OpenSans-SemiBold` and the ♀ in the Oswald test resolves to a real glyph.
    Two things had to be fixed to get there:
    - libass's fontconfig provider only reads the file named by `FONTCONFIG_FILE`; the
      fonts.conf mpv-android writes into its config dir is ignored ("No usable fontconfig
      configuration file found"). The app now exports the variable before libmpv loads,
      and the aliases (Arial -> Liberation Sans, Calibri -> Carlito, Myriad Pro -> Source
      Sans 3, ...) take effect: `(Arial, 400, 0) -> LiberationSans`.
    - Android's NotoColorEmoji is a CBDT bitmap font; libass cannot rasterise colour
      fonts (libass issue #381, still a feature request), so when fontconfig offered it for
      a missing glyph the glyph vanished. The config now rejects `*Emoji*.ttf` under
      /system/fonts and the OS CJK collection (which sorts its SC/TC faces ahead of JP),
      so fallback lands on the bundled monochrome Noto Emoji and Noto Sans CJK JP. The
      colour a glyph is drawn in comes from the ASS style anyway, never from the font.
    - `testdata/out/test-symbols-fallback.mkv` (no attachments) exercises all of this;
      every line rendered with no boxes (`apps/tv/spike-runs/20261014-symbols2/`).
    - Symbola 9.00 was added and tested first-in-chain (`20261014-symbols-symbola-first/`):
      it renders every symbol and emoji in the test except kana and post-2016 emoji, in a
      heavier hand-drawn style than Noto Emoji. `tools/font-coverage.py` over the test and
      the two real releases found no codepoint that only Symbola provides, so it sits
      after the Noto symbol fonts as the last-resort net. Across the real releases every
      non-ASCII codepoint (25 distinct: accented Spanish, arrows, ♀♂♪) is covered by
      DejaVu, Liberation, Source Sans and Noto CJK alike.
    - Still worth adding server-side: a "fonts referenced but not attached" report per
      ASS track from the style block, so the UI can warn before playback.
18. **Scene-style movie names needed a parser.** Both files landed as "episodes" with the
    raw filename as title until the server learned to treat a year-bearing file with no
    episode numbering as a movie, and to parse dotted release names
    (`Title.2019.1080p.x265-GRP`).
19. **Big files over Wi-Fi are fine at these bitrates** (3 MB/s KonoSuba, ~8 MB/s Toy
    Story), with Roxy's range streaming keeping 3 to 4 minutes of demuxer cache ahead and
    seeks landing within a second or two. A 4K remux at 80 Mbit/s is still untested.

## Decisions carried forward

- **Engine:** libmpv stays the primary engine. Nothing in this run argues for Media3 +
  ass-media; the comparison is only worth doing if the TV stutters.
- **hwdec default (provisional, pending TV):** try direct `mediacodec` first, fall back
  to `mediacodec-copy`, then software. Hi10P always ends up software and should be
  routed there directly to skip the failed probe.
- **Open for the TV run:** dropped frames at 1080p Hi10P + heavy typesetting; whether 4K
  HDR + libass holds 24 fps; 60 fps HEVC in hardware on the TV's SoC; whether the TV's
  MediaCodec HEVC decoder is also 10-bit capable; HDR10/HLG output with `vo=gpu` vs
  `gpu-next`; audio passthrough (TrueHD, E-AC-3, AC-3) over HDMI; 24 Hz refresh switching.
- **Track policy (new):** audio English first; subtitles chosen by server-detected
  language + title class, never by the default flag alone. A per-user "with English dub
  show: full subs | signs only | none" setting is a Phase 1b requirement.
- **Fonts:** bundle a fallback set of common fansub fonts in the app; add a server-side
  "referenced but not attached" font report per ASS track.
- **Still untested anywhere:** Dolby Vision (no synthetic sample possible; borrow a real
  profile 8.1 episode from the library) and VobSub.

## How this run was produced

```powershell
# Roxy Server on the laptop: ROXY_LISTEN=0.0.0.0:8096, media root testdata/out
cd apps\tv
.\tools\spike-matrix.ps1            # 5 files x {mediacodec-copy, mediacodec, no}, vo=gpu
```
