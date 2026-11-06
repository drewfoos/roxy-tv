# ADR-0007: Roxy clients speak the native API; Jellyfin compat is for the arr apps only

Status: accepted, 2026-10-13. Supersedes ADR-0006.

## Context

ADR-0006 had the TV app use `jellyfin-sdk-kotlin` against the compat surface so it could
run against the owner's existing Jellyfin before Roxy Server existed. By the time the
first APK ran on a phone, Roxy Server's direct-play MVP already existed, and the SDK cost
more than it saved in the first hour of real use:

- It needs an SLF4J binding at runtime or the first call throws.
- Its OkHttp transport is blocking, so every call needs an IO dispatcher.
- Its models are strict: every field Jellyfin marks required must be present, so the
  compat layer would have to reproduce Jellyfin DTOs field for field (first failure:
  `SessionInfo.SupportedCommands`), not just the fields the arr apps read.

Roxy is standalone. Its clients log into Roxy Server, never into Jellyfin.

## Decision

- Roxy clients (TV app, web) use the native `/v1` API (`docs/api/native.md`) with a small
  hand-written client (OkHttp + kotlinx.serialization, unknown keys ignored).
- The Jellyfin-compatible surface stays, scoped to what Sonarr and Radarr call.
  Making official Jellyfin clients work against Roxy is explicitly a non-goal for now.
- Migrating the owner's Jellyfin data (users, watched state, favourites) is a one-time
  import tool, later.

## Consequences

- The spike app dropped the Jellyfin SDK and SLF4J; the APK shrank by ~5 MB and the
  client is ~200 lines the project fully controls.
- Roxy Server gained native auth (`POST /v1/auth/login`, Bearer tokens, `?api_key=` for
  player-fetched media) and `/v1` is now authenticated.
- The compat layer no longer needs to chase Jellyfin DTO completeness.
