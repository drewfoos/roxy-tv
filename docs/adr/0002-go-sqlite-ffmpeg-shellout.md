# ADR-0002: Go, SQLite, and jellyfin-ffmpeg as a subprocess

Status: accepted, 2026-10-13

## Context

Options considered for the server: C#/.NET (reuse Jellyfin vocabulary, SubtitleEdit,
heavy runtime), Go (single binary, Kyoo's gocoder exists as a reference HLS transcoder),
Rust (no living media-server ecosystem; Dim is dead).

Jellyfin spent years retrofitting EF Core over scattered raw SQLite and still shipped
migration failures and performance regressions. Plex's library database slows as it grows.

jellyfin-ffmpeg ships GPL portable builds with the tested hwaccel matrix, libass burn-in,
`sub2video` and attachment fixes.

## Decision

- **Go** for Roxy Server. Single static binary, simple concurrency for ingest and
  transcode supervision, good HTTP range serving.
- **SQLite** via `modernc.org/sqlite` (no cgo), WAL mode, foreign keys, FTS5, `goose`
  migrations, `sqlc` queries. A real relational schema from day one.
- **jellyfin-ffmpeg** invoked as a subprocess for `ffprobe` and `ffmpeg`. Never linked.
  This inherits Jellyfin's hardware and subtitle fixes and keeps the GPL boundary at the
  process line.

## Consequences

- No ORM retrofit later; the schema is designed before the scanner is written.
- Transcoding argument builders are ported from Jellyfin's `EncodingHelper` patterns.
- Future option: use Kyoo's gocoder package for HLS if it saves time in Phase 3.
- The Docker image bundles jellyfin-ffmpeg and Noto CJK fonts.
