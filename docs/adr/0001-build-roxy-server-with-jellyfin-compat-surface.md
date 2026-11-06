# ADR-0001: Build Roxy Server from scratch, with a Jellyfin-compatible API surface

Status: accepted, 2026-10-13

## Context

The research (`docs/research/`) concluded that Jellyfin's server already handles ASS
correctly and that the subtitle failure is in the TV clients. It recommended building only
a client. The project owner wants to build the server too, to see whether a better one is
possible, and accepts the extra effort.

Sonarr and Radarr have a built-in Emby/Jellyfin connection. They talk to a small,
well-understood subset of Jellyfin's API.

## Decision

Build Roxy Server from scratch, but expose a **Jellyfin-compatible API subset** (shapes
of Jellyfin 12.x) as the integration surface for Sonarr, Radarr and v1 clients.
Roxy's own capabilities live on a separate native `/v1` API.

## Consequences

- Sonarr and Radarr work unchanged; the pipeline never notices the swap.
- The compat subset is a fixed, testable contract (`docs/api/jellyfin-compat.md`), not a
  moving target: Roxy implements what those integrations call, not all of Jellyfin.
- Roxy's internals are free to diverge (schema, decision engine, ingest) because only the
  wire format is Jellyfin-shaped.
- Risk: Jellyfin changes shapes in a future major and the integrations follow. Mitigation:
  replay tests built from captured real traffic, re-captured on each integration release.
