# ADR-0006: The Roxy TV app v1 speaks the Jellyfin-compatible API

Status: superseded by ADR-0007 on 2026-10-13 (the app speaks Roxy's native API; the
Jellyfin SDK was dropped after it proved costly in the first real run).

## Context

Roxy Server must implement a Jellyfin-compatible subset anyway (ADR-0001). The owner has a
working Jellyfin 12.2 today and a TV that already proves client-side decoding works. The
fastest way to get value, and to validate libmpv on the real TV, is a client that works
against Jellyfin before Roxy Server exists.

## Decision

- The TV app's v1 backend adapter targets the Jellyfin 12.x API (via `jellyfin-sdk-kotlin`)
  for login, device-code login, browsing, `PlaybackInfo`, streaming, attachments, sidecar
  subtitles and playback reporting.
- Roxy Server implements the same calls in its compat surface, so the app moves over with
  a URL change.
- Roxy-only features (explainable decision traces, ordering preferences, policies) arrive
  later through the native `/v1` API behind capability discovery (`GET /v1/capabilities`
  present -> enable; absent -> Jellyfin mode).

## Consequences

- Phase 1 delivers a usable client months before the server, on the existing stack.
- The compat surface gets a second, demanding consumer (the app) alongside the arr
  apps, which hardens it.
- The app's adapter layer must stay thin and backend-agnostic so the native API can be
  slotted in without a rewrite.
