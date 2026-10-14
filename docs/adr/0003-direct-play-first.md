# ADR-0003: Direct play first; transcoding is a fallback, not the MVP

Status: accepted, 2026-10-13

## Context

Hi10P H.264 has no hardware decoder on any server GPU or TV box. libass rasterization is
CPU-bound even on hardware transcode paths. Kodi and mpv on the owner's budget Android TV
already direct-play Hi10P with styled ASS by decoding and rendering on the TV's CPU.

The owner's server is a Ryzen 5800X with a Radeon RX 550 (H.264 and 8-bit HEVC hardware
encode, HEVC 10-bit decode, no AV1, no Hi10P decode).

## Decision

- Clients decode and render. The Roxy TV app ships a software decoder (via libmpv) and
  libass, and declares those capabilities in its profile.
- Roxy Server's MVP serves bytes: range requests, attachments, sidecar subtitles. No
  transcoder in Phase 2.
- Transcoding (Phase 3) exists for devices that cannot keep up: weak sticks, phones on
  cellular, Roku/webOS/Tizen. It is selected per session by the decision engine or by
  the client's runtime dropped-frame fallback.

## Consequences

- The server MVP is small enough to build in months.
- The 5800X + RX 550 is sufficient: GPU-decoded HEVC, CPU libass, GPU 8-bit encode for
  the occasional fallback stream. An Arc A310 is only needed for concurrency, HDR tone
  mapping, AV1 or 10-bit encode.
- The client must implement the runtime fallback, which Kodi lacks.
