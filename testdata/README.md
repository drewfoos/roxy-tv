# Test media

Synthetic, anime-shaped files for developing and testing Roxy without a real library.
Generate with:

```powershell
python tools\make-pgs.py --ffmpeg ~\tools\jellyfin-ffmpeg\ffmpeg.exe --font fonts\Oswald.ttf --out subs\bluray.sup
.\make-test-media.ps1            # all files, ~60 s each (4K capped at 20 s)
.\make-test-media.ps1 -SkipSlow  # skip the 4K HDR10 and AV1 encodes
.\make-test-media.ps1 -Only test-bluray-pgs.mkv   # just one
```

Requires the jellyfin-ffmpeg portable build (default path `~/tools/jellyfin-ffmpeg`),
Python 3 for the PGS generator, and the two fonts in `fonts/` (git-ignored; see below).

| File | Video | Audio | Subs | Proves |
|---|---|---|---|---|
| `test-hi10p-ass.mkv` | H.264 High 10, 1080p23.976 | AAC jpn | ASS + 2 font attachments | software decode + libass |
| `test-hevc10-ass.mkv` | HEVC Main10, 1080p | FLAC jpn | ASS + fonts | hwdec 10-bit + libass |
| `test-dualaudio-ass.mkv` (+ `.en.ass`, `.en.forced.srt` sidecars) | HEVC Main10 | FLAC jpn, E-AC-3 eng | ASS + SRT embedded, Bazarr-style sidecars | track selection, sidecar parsing |
| `test-hevc10-hdr10-4k-ass.mkv` | HEVC Main10 HDR10 PQ/BT.2020, 2160p, 20 s | E-AC-3 | ASS + fonts | 4K hwdec, HDR out, libass at 4K |
| `test-av1-10bit-ass.mkv` | AV1 10-bit, 1080p, 30 s | Opus | ASS + fonts | AV1 decode |
| `test-h264-8bit-ass.mkv` | H.264 High 8-bit, 1080p | AAC jpn | ASS + fonts | the most common format; hwdec baseline |
| `test-hevc8-60fps-51-ass.mkv` | HEVC Main 8-bit, 1080p**60** | FLAC 5.1 jpn, E-AC-3 5.1 eng (distinct tone per channel) | ASS + fonts | high frame rate hwdec, multichannel decode / passthrough / channel mapping |
| `test-bluray-pgs.mkv` | H.264 High 8-bit, 1080p | TrueHD 5.1 jpn, AC-3 5.1 eng | **PGS** bitmap (`subs/bluray.sup`, 6 cues) | Blu-ray remux shape: bitmap subs, lossless + core audio |
| `test-hevc10-hlg-ass.mkv` | HEVC Main10 HLG/BT.2020, 1080p | AAC | ASS + fonts | the other HDR flavour, no static metadata |
| `test-h264-mp4-movtext.mp4` | H.264 High 8-bit, 1080p, MP4 | AAC stereo | mov_text (3GPP timed text) | non-MKV container, text subs without attachments |
| `test-symbols-fallback.mkv` | H.264 High 8-bit, 1080p, 30 s | AAC stereo | `subs/symbols.ass`, **no fonts attached** | player font fallback: Open Sans + Arial by alias, ♀♂♪★♥→, JP punctuation incl. wave dash vs fullwidth tilde, emoji, box drawing, kana/kanji/Hangul/Hanzi, Greek/Cyrillic |

Not synthesizable here: **Dolby Vision** (needs an RPU; borrow a real profile 8.1 episode
from the library once the server is back) and **VobSub** (ffmpeg's `dvdsub` encoder needs
bitmap input). Both are detected by Roxy's probe already; only playback is untested.

The PGS stream is written by `tools/make-pgs.py` (ffmpeg has no PGS encoder): it renders
each cue with drawtext, quantises to a 4-colour palette, RLE-encodes, and emits the
PCS/WDS/PDS/ODS/END segments with a clearing set at each cue end. Each cue's text says
what correct rendering looks like.

Every file has a burned-in timecode and frame counter at the top-left so sync and seek
accuracy are visible, and the ASS track (`subs/typeset.ass`) describes on screen what
each line should look like when rendered correctly.

## Fonts

- `fonts/NotoSansCJKjp-Regular.otf` from https://github.com/notofonts/noto-cjk (OFL)
- `fonts/Oswald.ttf` from https://github.com/google/fonts/tree/main/ofl/oswald (OFL)

Oswald is deliberately a font no TV ships, so a correctly rendered gold "ROXY TV SIGN"
line proves the player loaded the MKV attachment rather than falling back.
