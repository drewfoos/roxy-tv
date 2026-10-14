# Roxy TV

A self-hosted media server and client pair built for anime: styled ASS subtitles rendered
correctly on the TV, 10-bit HEVC and Hi10P direct play, per-user accounts, and drop-in
compatibility with request app, Sonarr, Radarr and download client.

```
user -> request app -> Prowlarr -> Radarr / Sonarr -> download client -> /data/media -> Roxy Server -> Roxy clients
```

## Components

| Component | Path | Stack | Role |
|---|---|---|---|
| Roxy Server | `server/` | Go, SQLite, jellyfin-ffmpeg (shell-out) | Library, metadata, users, playback decisions, streaming, transcoding fallback |
| Roxy TV app | `apps/tv/` | Kotlin, Compose for TV, libmpv (primary) + Media3 (secondary) | Android TV / Google TV / Fire TV client with client-side libass |
| Roxy Web | `apps/web/` | TypeScript, jassub, HLS.js | Browser client |
| Deploy | `deploy/` | Dockerfile, Docker Compose | Container-first: multi-arch image with jellyfin-ffmpeg and fonts, reference compose stack alongside the arr apps |

## Documents

- `docs/ARCHITECTURE.md` — the system design. Start here.
- `docs/ROADMAP.md` — phases, milestones, exit criteria.
- `docs/adr/` — architecture decision records (why things are the way they are).
- `docs/api/jellyfin-compat.md` — the Jellyfin-compatible API subset Roxy Server must implement for request app, Sonarr and Radarr.
- `docs/api/arr-webhooks.md` — the Sonarr/Radarr webhook payloads the scanner consumes.
- `docs/research/` — the research report and source notes this design is based on (October 2026).

## Naming

The project is **Roxy TV**. The backend is **Roxy Server**, the Android TV client is the
**Roxy TV app**, the browser client is **Roxy Web**. The bare name `roxy` is taken by several
reverse-proxy projects and a GitHub user, so publish under a namespace such as `roxy-media`
(GitHub org, Docker Hub, package scopes).

## Status

Planning. No code yet. First step is the player spike in `docs/ROADMAP.md` (Phase 1a):
prove libmpv + libass on the living-room TV before anything else.
