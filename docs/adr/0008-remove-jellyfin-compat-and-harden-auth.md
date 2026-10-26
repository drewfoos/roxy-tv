# ADR-0008: Remove the Jellyfin-compatible API; harden sign-in and media URLs

Status: accepted, 2026-10-26. Supersedes the compat parts of ADR-0001 and ADR-0007.

## Context

ADR-0001 added a Jellyfin-shaped API so request app, Sonarr and Radarr could treat Roxy as a
Jellyfin server. Since then Roxy grew its own Discover / requests (request app's job) and the
arr apps report imports through Roxy's native webhooks (`/v1/webhooks/{source}`). Roxy's
clients never used the compat surface (ADR-0007). It had become dead weight and a second
sign-in door, with its own token header formats.

A review of how the app talks to the server also found:

- no limit on password guessing beyond the general request rate;
- the request rate limit keyed on the bearer token, so a new made-up token per request
  got a fresh allowance;
- a missing account in sign-in answered faster than a wrong password (user enumeration);
- `CheckPassword` accepted a blank password for an account with no hash;
- a first start without `ROXY_ADMIN_PASSWORD` created the admin with password `roxy`
  (and the Docker compose file passed `roxy` explicitly);
- `GET /stream/{id}` played any file with no sign-in (a player-spike leftover);
- the account token went into every media URL (`?api_key=`), which is where an external
  player or a log picks it up;
- any signed-in user could force a metadata refresh.

## Decision

- Delete the Jellyfin-compatible API (`server/internal/api/compat`, `docs/api/jellyfin-compat.md`)
  and the unauthenticated `/stream/{id}` route.
- Passwords: bcrypt cost 12 (salted by bcrypt; older hashes upgraded at the next sign-in),
  blank passwords never match, a missing account costs the same bcrypt compare.
- Sign-in throttling per account and per address: 5 free misses in 15 minutes, then waits
  of 30 s doubling to 15 min (429 with Retry-After). A correct password clears the account.
- Rate limit: per token (20/s, burst 60) and per address on top (3x that), so inventing
  tokens does not escape it.
- First start without `ROXY_ADMIN_PASSWORD`: a random 20-character admin password, logged once.
- Media tokens: `GET /v1/sources/{id}/token` returns an HMAC-signed token for one source
  and one user, valid 24 h, accepted (as `?mt=`) only on that source's stream, transcode,
  subtitles, attachments and trickplay. Players get these URLs; the account token stays
  in headers. Downloads send the account token as a header.
- Metadata refresh of an item is admin only; trickplay ids are validated before they name a folder.

## Consequences

- Official Jellyfin apps and request app can no longer connect. Bringing them back means
  restoring the package from git history (before 2026-10-26).
- The TV's external player can be handed a URL that exposes one video for a day, not the account.
- Old `?api_key=` media URLs from earlier app builds stop working only where they used the
  removed routes; `/v1/stream` still accepts `api_key` for API keys (arr integrations).
