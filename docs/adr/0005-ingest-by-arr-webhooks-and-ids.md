# ADR-0005: Ingest is driven by Sonarr/Radarr webhooks and IDs, not filename parsing

Status: accepted, 2026-10-13

## Context

Jellyfin and Plex both discover files by walking the filesystem and parsing names with
regexes, then match them against metadata providers. For anime this is where absolute
numbering, season splits and wrong-show matches come from, and why Shokofin exists.

In this pipeline every file arrives through Sonarr or Radarr, which already know the
TVDb/TMDb/IMDb IDs, season and episode numbers (and, for Sonarr, MAL and AniList IDs),
and emit a webhook with the final path on import, upgrade, rename and delete.

## Decision

- The Sonarr/Radarr generic Webhook connect is the primary ingest source. Items are
  created and keyed from the IDs in the payload; the file is attached from the path.
- `POST /Library/Media/Updated` (the built-in Emby/Jellyfin connect) is also supported.
- A filesystem watcher handles Bazarr sidecars and manual drops.
- An hourly reconciliation poll against the arr `/api/v3` endpoints repairs missed events.
- Filename regexes (TRaSH tokens) are a fallback for manual drops only.
- AniDB/AniList/MAL IDs are resolved via a locally cached Fribb/anime-lists mapping.

## Consequences

- Near-zero mismatch risk; metadata providers only fetch, they never guess.
- Aired, absolute and AniDB numbering can all be stored because the mapping is known.
- Roxy depends on the arr apps for discovery. A library not managed by Sonarr/Radarr gets
  the fallback path and the weaker guarantees that come with it.
- Ingest is idempotent and auditable (`ingest_events` table) so webhooks can be replayed.
