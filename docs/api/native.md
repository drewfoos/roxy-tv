# Roxy native API (v1)

The API Roxy's own clients (Roxy TV app, Roxy Web) speak. It is deliberately small and
lenient: clients read the fields they need and ignore the rest, so the server can add fields
without breaking anyone. JSON, camelCase, ISO-8601 times, milliseconds for durations.

There is no Jellyfin-compatible surface any more (ADR-0008): Roxy's clients, Discover and
the Sonarr / Radarr webhooks all use this API.

## Auth

| Method | Path | Body / result |
|---|---|---|
| POST | `/v1/auth/login` | `{username, password, client, device, deviceId, version}` -> `{token, user: {id, name, isAdmin}, deviceId}` |
| GET | `/v1/auth/me` | `{id, name, isAdmin}` or `{apiKey: true, isAdmin: true}` |
| POST | `/v1/auth/logout` | revokes the calling device token, clears the session cookie, 204 |
| POST | `/v1/auth/cookie` | sets `roxy_token` (HttpOnly, SameSite=Strict, a year) carrying the caller's device token, 204; for Roxy Web, whose `<img>` and `<video>` cannot send a header |

- The cookie is honoured for **GET and HEAD only** (posters, video, subtitles, fonts). Anything
  that changes state still needs the Bearer header, so a page on another site cannot act
  through the cookie. API keys and media tokens are never put in it.

- The token is an opaque 64-hex device token, stored hashed (SHA-256).
- Send it as `Authorization: Bearer <token>`. Media URLs a player fetches on its own carry
  a media token instead (`GET /v1/sources/{id}/token`, then `?mt=<token>`): one source,
  24 hours, only its stream / transcode / subtitles / attachments / trickplay.
- Passwords are bcrypt (cost 12). Wrong passwords are throttled per account and per address:
  5 free misses in 15 minutes, then 429 with `Retry-After` (30 s, doubling to 15 min).
- Integration API keys (`/v1/auth/keys`, for the arr webhooks) are accepted everywhere a
  device token is, and as `?api_key=`.
- Unauthenticated calls to anything but `system/info` and `auth/login` get `401 {"error": ...}`.

## Finding the server

Clients list servers on the login screen two ways at once: a UDP broadcast of the text
`who is roxy?` to port 8096, which the server answers with
`{id, name, product: "roxy", version, port, address, advertised}` (the client builds the
URL from the reply's source address and `port` unless `advertised` is true, when
`address` is `ROXY_ADVERTISE_URL`), and an HTTP sweep of the phone's subnet reading
`GET /v1/system/info` on 8096 and keeping replies whose `product` is `roxy`. The sweep
is the fallback for networks that drop broadcasts (Docker bridge, Wi-Fi client isolation).

## More like this

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/items/{id}/similar` | `{items: [Discover Item]}`: TMDB's recommendations for a library title (its "similar" list when there are none), library titles marked; empty when TMDB does not know the title. Cached an hour. |

## People

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/people/{tmdbId}` | TMDB's person: `{tmdbId, name, biography, birthday, deathday, placeOfBirth, knownFor, photo, homepage, imdbId, credits: [Discover Item + character]}`. Credits are one per title, best known first; `libraryItemId` marks what the library has. Cached an hour. The cast on `/items/{id}/details` carries `tmdbId` to link here. |

## Sessions ("Play on")

The phone picks a player signed in to the same login and the server relays commands to
it; the player plays with its own engine (the TV app: libmpv + libass). In memory only.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/sessions/commands?name=&client=&wait=` | A player's long poll: `{command}` or 204 after `wait` s (25 max). Polling keeps it listed; 60 s without a poll drops it. |
| POST | `/v1/sessions/state` | `{itemId, title?, positionMs, paused, seq?}`; empty `itemId` = stopped. `seq` is the player's increasing number for its reports: one not newer than the last accepted is ignored (reports can overtake each other); omitted = always accepted |
| GET | `/v1/sessions/players` | `{players: [{deviceId, name, client, lastSeen, nowPlaying?}]}`, the login's other players |
| POST | `/v1/sessions/players/{device}/command` | `{type, itemId?, positionMs?, fromStart?, seconds?, from?}`; `type` = play, pause, resume, toggle, seek, seekBy, next, stop. 202, or 404 when not connected |

`play` resumes from the saved position; a sender handing over mid-episode sets
`positionMs`, which the player saves first. `positionMs` or `fromStart: true` is a position the sender chose: a player that already has that item open jumps there instead of carrying on. Commands need a device login (not an API key).

## Library

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/system/info` | `{product: "roxy", id, name, version, ffmpeg}`; public, and what the login screen's network sweep reads |
| GET | `/v1/libraries` | `{libraries: [{id, name, kind, root, animeMode}]}` |
| POST | `/v1/libraries/scan` | async full rescan, 202 |
| GET | `/v1/items` | `{items: [Item], total}`; query below |
| GET | `/v1/items/{id}` | Item with `mediaSources` |
| GET | `/v1/sources/{id}` | MediaSource |

`/v1/items` query: `type=episode,movie,series,season` · `parent=<id>` · `recursive=1` ·
`library=<id>` · `sort=date_created|title|index` · `desc=1` · `limit` · `offset` ·
`include=sources` (attach `mediaSources` to each item).

Item: `{id, guid, libraryId, type, parentId?, title, sortTitle?, year?, overview?,
indexAired?, seasonAired?, indexAbsolute?, runtimeMs?, providerIds?, dateCreated,
dateModified, seriesId? + seriesTitle? (episodes, list views), mediaSources?}` plus the
metadata, `images` and `userData` fields described below.

MediaSource: `{id, itemId, name?, container?, size, durationMs, bitrate?, versionLabel?,
isPrimary, parts[], streams[], attachments[]}`. Streams carry `type` (`video|audio|subtitle`),
`codec`, `profile?`, `bitDepth?`, `width?`, `height?`, `fps?`, `rangeType?`
(`SDR|HDR10|HDR10+|HLG|DV`), `language?`, `detectedLanguage?`, `title?`, `isDefault`, `isForced`, `isExternal`,
`isTextFormat`. Attachments carry `index`, `filename`, `mime?`.

`detectedLanguage` (ISO 639-2/B) is sniffed from the text of subtitle tracks at ingest.
Clients should trust it over `language`: release groups mislabel tracks (two "Dialogue"
tracks both tagged `eng`, one of them Spanish, was found in the first real file tested).
It is absent for bitmap subtitles and for tracks too short to classify.

## Media

| Method | Path | Notes |
|---|---|---|
| GET/HEAD | `/v1/stream/{sourceId}` | the file, HTTP ranges honoured (`206`) |
| GET | `/v1/sources/{id}/attachments/{index}` | attachment bytes (fonts), extracted eagerly at scan |
| GET | `/v1/sources/{id}/subtitles/{index}` | text subtitle track, byte for byte |
| GET | `/v1/sources/{id}/transcode?height=&bitrate=&start=` | the source re-encoded, as a Matroska stream (see Transcoding). `web=1` (Roxy Web) carries one audio track (`audio=<stream index>`, else the first) in a browser codec, no subtitles, and restarts timestamps at zero at `start`. |
| GET | `/v1/sources/{id}/remux?audio=&start=` | the video copied untouched with one audio track (`audio` = the track's stream index; AC3, E-AC3, DTS, TrueHD and PCM become AAC, the rest are copied), as Matroska with timestamps restarting at zero at `start`. For browsers, which cannot switch MKV audio tracks or decode those codecs. Eight at a time, then 503. |

## Transcoding

`GET /v1/sources/{id}/transcode?height=720&bitrate=4000&start=300.5` (Bearer or
`?api_key=`, since libmpv cannot send headers) streams the file re-encoded to H.264 at up
to `height` lines and `bitrate` kb/s of video, from `start` seconds, as **Matroska over a
plain 200 response** (no ranges, no length). It is for Roxy's own libmpv players, which
keep rendering ASS themselves: every subtitle track, font attachment and chapter is copied
as-is (MP4 `mov_text` becomes SRT), so it plays like direct play at a lower bitrate.

- Timestamps keep the file's clock (`-copyts`): the first frame of a stream started at
  300 s is at 300 s. Players set mpv `rebase-start-time=no` and take the duration from the
  source (`durationMs`), since the stream has none.
- Seeking inside what the player has buffered is local; anything else is a new request
  with a new `start`. Closing the connection stops the server's ffmpeg.
- Audio: every track stays (dub/sub switching is instant). At 720p and above, compact
  tracks (AAC, AC3, E-AC3, Opus, MP3 up to 640 kb/s) are copied, so 5.1 and soundbar
  passthrough survive; FLAC, TrueHD, DTS and PCM become AAC (5.1 at 384 kb/s, or stereo
  at 160 kb/s). At 480p-class settings (height 480 or less, or under 2.5 Mb/s) every
  track is stereo AAC at 128 kb/s.
- HDR sources are tone-mapped to SDR (`tonemapx`) when the build has the filter.
- At most 4 transcodes run at once; a fifth gets `503`.
- The apps offer 4K (40 Mb/s), 1440p (16), 1080p (8), 720p (4) and 480p (1.5), listing
  only the ones below the file's own resolution; the file itself is shown as its
  resolution ("1080p", "4K"). A default per network (Wi-Fi, mobile data) lives in the
  app's settings and is skipped for a file whose bitrate is already below it.
- `GET /v1/system/transcoding` -> `{encoder}`: the encoder in use (`nvenc`, `qsv`,
  `vaapi`, `amf` or `software`).

### Transcoding settings (admin)

- `GET /v1/settings/transcoding` -> `{hwAccel, encoder, options: [{id, label, available,
  suggested?}]}`. `hwAccel` is the saved choice (`auto` or one encoder), `encoder` what
  runs now. Every encoder is test-encoded once (a few seconds on the first call, cached
  after); `available` says which work. The `auto` option's label names what Auto picks on
  this server, e.g. `Auto (NVIDIA NVENC)`, and `suggested` carries its id.
- `PUT /v1/settings/transcoding` `{hwAccel}` -> same as GET. A hardware encoder that fails
  its test is refused with `422` and nothing is saved. The saved choice wins over
  `ROXY_HWACCEL` (which only sets the default); if the saved encoder is gone at startup
  (another machine, a container without the device) the server falls back to Auto.

## Metadata and images (Phase 1b contract)

Items gain, when a provider matched them: `overview`, `genres[]`, `tagline?`,
`communityRating?` (0-10), `officialRating?` ("TV-14"), `premiered?` (date), `status?`
(series: "Continuing" | "Ended" | "Upcoming"), `studio?`, `metadataRefreshedAt?`,
`providerIds` now including `imdb`, `anilist`, `mal`, `tvdb`, `tmdb`, and
`images: {poster?, backdrop?, logo?, thumb?}` whose values are relative URLs
`/v1/items/{id}/images/{type}`. Episodes get real titles, `overview`, `airDate?` (also
present as `premiered`) and a `thumb` still. Seasons get `poster`. Absent keys mean "no
image"; clients must render a placeholder and never 404-spam. The title of a matched
movie/series is the provider's (English where AniList has one), and a rescan does not
revert it.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/items/{id}/images/{type}` | `type` = poster, backdrop, logo, thumb, hero (a textless poster for the phone's Home banner; TMDB's and fanart.tv's language-less renditions, set when one exists). Backdrops prefer textless renditions too, since the clients draw the logo over them. Served from the local cache (`data/cache/images/<item>/<type>.jpg`), `Cache-Control: public, max-age=86400`, 404 if none. Optional `?w=<px>` (16-4096, never enlarges) returns a resized JPEG cached as `<type>.w<px>.jpg`. Auth: Bearer or `?api_key=`. |
| POST | `/v1/items/{id}/refresh` | re-run metadata lookup for the item (its series when given a season/episode; children are refreshed with it), 202. 404 for an unknown item. |
| GET | `/v1/home` | `{rows: [{key, title, items: [Item]}]}` in display order: `continue` (resume list, empty for API keys), `recentEpisodes` (latest 20 episodes by date added, one per series, each with `seriesId` + `seriesTitle`; fetch the series poster via `/v1/items/{seriesId}/images/poster`), `recentMovies` (20), `series` (all, by date added, 50), `movies` (all, 50). Each item carries `images` and `userData`, so the home screen needs one request. |

Providers: TMDB for movies and series (API key via `ROXY_TMDB_API_KEY`; disabled with
one log line when unset), AniList for anime (no key): it leads when the library has
`animeMode`, otherwise runs after TMDB as fallback, and is always consulted so a match
carries its `anilist` id. Lookup runs asynchronously after ingest (and sweeps every
series/movie after a full scan, skipping anything refreshed within 7 days) and uses
`providerIds` from the filename/webhook first, title + year search second; a search hit
is accepted only when the normalized title is close enough, so unmatched names stay
unmatched. TheTVDB is not called; TVDB ids from Sonarr are resolved through TMDB's
`/find`. AniList episode titles/thumbs come from its `streamingEpisodes` for the matched
entry (season 1); multi-season anime get later seasons from TMDB only, for now.

## Missing and unaired episodes (placeholders)

After a series is matched, the server creates **virtual** episodes (and seasons) for
every episode the provider lists that the library has no file for, so clients can show
Jellyfin-style gaps. Specials (season 0) placeholders are only created when the library
already has a Specials season with a real file, because provider specials lists are long
and mostly recaps.

- `isVirtual: true` marks a placeholder; `hasMedia` says whether any media source exists.
- `availability` on episodes and movies: `available` | `missing` | `unaired` (air date in
  the future). Clients render missing/unaired rows greyed with a badge and must not try
  to play them (`/v1/stream` would 404).
- Placeholders share the scanner's GUIDs, so when the file arrives the same row flips to
  real: ids, watch state and images are kept.
- `GET /v1/items?...&hasMedia=1` hides placeholders; `hasMedia=0` lists only them (a
  "wanted" view). `/v1/home` rows only ever contain real items.
- `GET /v1/items?...&favorites=1` restricts to the caller's favourites (the client's
  Watchlist; 401 without a user login).
- `/v1/home` → `continue` lists **one entry per show** (the most recently played
  episode of each series, plus movies) and carries `mediaSources` so the client can show
  Dub | Sub and play without another round trip. `/v1/me/resume` applies the same rule.
- Logos are stored and served as PNG (transparency kept); other images are JPEG. The
  image endpoint's `Content-Type` follows the file.
- Rate limiting: the JSON API allows 20 requests/s sustained with a burst of 60 per
  client (IP + token), answering `429` with `Retry-After: 1` beyond that. Media bytes
  (`/v1/stream`, images, trickplay, attachments, subtitles) are exempt.
- Placeholders the provider stops listing are removed on the next refresh.

## Details page

`GET /v1/items/{id}/details` (a season or episode id resolves to its series) →
`{item, facts: {files, bytes, video[], audio[], subtitles[], subFormats[]}, tmdb:
{originalTitle, firstAired, lastAired, status, episodes, seasons, runtimeMinutes,
networks[], companies[], countries[], languages[], keywords[], cast[{name, character,
photo}], homepage, tagline}, links: {tmdb, imdb, anilist, mal, tvdb}}`. The TMDB part is
cached 10 minutes. Profiles carry `avatarKey` (a hash of the picture's source); clients
put it on the avatar URL so a changed picture is never served from an image cache.

## Segments (skip intro / outro)

`MediaSource.segments[]`: `{type: intro | outro | recap | preview, startMs, endMs,
origin}`. Derived at ingest from the file's chapter markers (`origin: "chapters"`): among
55..125 s chapters starting within the first 4 minutes, the one closest to 90 s (the
length of nearly every TV anime OP/ED; a cold open can be 60..120 s too, so "first that
fits" is wrong) is the intro; the same rule over chapters starting in the last 5 minutes
gives the outro; a chapter of 45 s or less right after the outro is the preview. Movies (long chapters only) get none. `POST
/v1/libraries/scan?force=1` re-probes unchanged files, which is how existing items pick
segments up after a probe change. Planned origins: `fingerprint` (audio match across a
season for files without chapters) and `manual`.

## Trickplay (seek-preview thumbnails)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/sources/{id}/trickplay` | `200` manifest `{intervalMs, width, height, columns, rows, count, sheets, generatedAt}`; `202 {"status":"generating"}` after queueing (retry later); `404` unknown source |
| GET | `/v1/sources/{id}/trickplay/{n}` | sheet `n` (0-based), JPEG, immutable cache headers |

Generated server-side after ingest (one keyframe every 10 s, 320 px wide, 10×10 tiles
per sheet, `data/cache/trickplay/<source>/`) and swept for missing sources at startup.
Tile for time `t`: index `floor(t / intervalMs)`, sheet `index / (columns*rows)`, column
`(index % per_sheet) % columns`, row `(index % per_sheet) / columns`.

## Related titles (franchise view)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/items/{id}/related` | `{related: [RelatedEntry], partOf: [Item]}` |

`related` lists the provider's linked titles that are not seasons of this item: movies,
OVAs, side stories, spin-offs, alternative versions, summaries. Each entry has
`relation` (`movie | ova | special | side_story | spin_off | alternative | summary |
sequel | prequel | parent | other`), `provider` + `providerId`, `title`, `format`, `year`,
`inLibrary`, and either `item` (the library Item, with `userData`) or `posterUrl` (a
provider-hosted image for titles not in the library). `partOf` is the reverse link: the
library series whose relations include this item, so a movie page can show "Part of
KONOSUBA". Crossover relations (AniList `CHARACTER`) and source material are excluded.

Seasons: AniList models every TV season as its own entry; the server follows the
PREQUEL/SEQUEL chain so Season n maps to the n-th entry (`SeasonInfo.ProviderID`). When
AniList repeats one season's Crunchyroll episode list on another entry (a known data
problem), that season is marked untrusted and left to another provider (TMDB) to fill;
a forced refresh resets any wrong episode titles it wrote earlier. Numbering rule:
**the file's season/episode numbering is the truth**; the provider maps onto it, Specials
is only for files named `S00Exx`, and nothing is auto-moved.

## Watch state (native)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/me/resume?limit=` | `{items: [Item]}` with `userData.positionMs > 0` and not played, most recent first (default 20) |
| POST | `/v1/me/progress` | `{itemId, positionMs, audioIndex?, subtitleIndex?}` → 204. Marks played (and resets the position) at >= 90 % of runtime. On a played item, a position past 30 s makes it in progress again (play count kept); a shorter one is ignored, so it stays played. 404 for an unknown item. |
| POST / DELETE | `/v1/me/played/{itemId}` | set / clear played, returns `{itemId, userData}` |
| POST / DELETE | `/v1/me/favorite/{itemId}` | set / clear favourite, returns `{itemId, userData}` |

Every Item in a list, detail, home or resume response for a user token carries
`userData: {positionMs, played, playCount, isFavorite, lastPlayedAt?, audioIndex?,
subtitleIndex?}` (one batched query per response). API-key callers get no `userData`,
and the `/v1/me/*` endpoints answer them 401.

## Profiles

Several viewers on one login (Netflix-style). Watch state, favourites, `/v1/me/resume`
and `/v1/home` are **per profile**. Clients send `X-Roxy-Profile: <id>` on every request;
without it (or with a profile that is not the caller's) the user's default profile is
used. Migration 0007 gave every
existing user a default profile that inherited their history. At most 6 per account.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/me/profiles` | `{profiles: [Profile], current}`; `current` is the id this request resolved to |
| POST | `/v1/me/profiles` | `{name, avatarUrl?, avatarName?}` → 201 Profile |
| PATCH | `/v1/me/profiles/{id}` | `{name?, avatarUrl?, avatarName?}`; `avatarUrl: "-"` clears the picture |
| DELETE | `/v1/me/profiles/{id}` | 204; the last profile cannot be deleted, deleting the default promotes the oldest |
| GET | `/v1/me/profiles/{id}/avatar?w=` | the cached picture (JPEG), 404 when none |
| GET | `/v1/avatars/search?q=` | `{characters: [{id, name, image, series}]}` from AniList |
| GET | `/v1/avatars/suggested` | main cast of the library's series (up to 6 per show, cached an hour) |

`Profile: {id, name, isDefault, avatarName, hasAvatar, avatarUrl, createdAt}`. Avatars are
anime characters: `avatarUrl` on create/update must be an AniList CDN image (as returned
by the two `/v1/avatars` endpoints); the server caches a copy and serves it from
`/v1/me/profiles/{id}/avatar`. `/v1/auth/me` also returns `profileId`.

## Discover (requests)

TMDB through the server (the key stays there; posters are absolute `image.tmdb.org`
URLs the app loads directly). Every item carries `libraryItemId` when the library has
it (matched on the TMDB id) and `requestStatus` (`pending` | `sent` | `failed`) when
someone asked for it. Rows are cached 30 min, details 10 min, searches 5 min, and the
TMDB client has its own token bucket, so the app cannot hammer TMDB.

- `GET /v1/discover` → `{rows: [{key, title, items}], requestsEnabled: {tv, movie}}`.
  Rows: trending, new anime this season, popular anime, top rated anime, anime movies,
  popular movies, upcoming movies, popular series.
- `GET /v1/discover/search?q=` → `{items}` (TV and movies).
- `GET /v1/discover/{tv|movie}/{tmdbId}` → the item plus `tagline, status, seasons,
  episodes, runtimeMinutes, networks, tvdbId, imdbId, request, requestsEnabled`.
- `POST /v1/discover/request` `{type, tmdbId}` → the request row. Sent to Sonarr
  (by TVDB id) or Radarr (by TMDB id) once that app is set up for requests: an address,
  a key, **and** a root folder and quality profile chosen in the settings (Account →
  Sonarr & Radarr). An address and key alone, say from the `ROXY_*` variables, do not
  turn requests on. Without it the request is refused with `412` and a message naming
  the app; the apps hide the Request button (admins get a link to the settings page).
  409 when the library already has it.
  - Anime (TMDB: Animation + Japanese) goes to the anime root folder and profile, on
    Sonarr and Radarr alike, with Sonarr's series type from the settings (`standard`
    by default). Older anime (Sonarr's lookup: first aired before 2010, status ended)
    goes to `oldAnimeQualityProfileId` when one is set; a long runner still airing keeps
    the anime profile.
  - A new series is added with Sonarr's own search on add **off** (it searches episode
    by episode, never matches a season pack, grabs one stray episode, and that file then
    blocks the pack as a downgrade). With `searchOnAdd` on, Roxy then sends a
    `SeriesSearch` for a whole-series request or a `SeasonSearch` per requested season.
  - A series Sonarr already has and monitors gets the requested seasons monitored and
    searched. One Sonarr has but does **not** monitor is kept by hand there (subtitles
    only, a few episodes): the request fails with "already in Sonarr and not monitored
    there (kept by hand)" and nothing in Sonarr changes.
- `GET /v1/discover/browse?type=tv|movie&sort=popular|top|newest|oldest|votes&genres=16,35&lang=ja&anime=1&yearFrom=&yearTo=&minRating=&status=airing|ended&page=`
  → `{items, page, totalPages}` (TMDB discover; cached 5 min per query; `anime` =
  Animation + Japanese original language). `GET /v1/discover/genres?type=` → `{genres}`.
- `GET /v1/discover/requests` → `{items}` (everyone's, newest first).
- `POST /v1/discover/requests/retry` (admin) → resend pending requests now.

### Season layout

TMDB keeps many anime as one long season (The Apothecary Diaries: "Season 1, 60
episodes", every cours together), while TVDB, and so Sonarr, numbers them 1, 2, 3. When
TMDB has a "Seasons" episode group (production or air-date order) that splits the show
into more seasons and covers about the same episodes, the detail uses it:
`seasonLayout: "episodeGroup"` (else `"tmdb"`), `seasons` and
`GET /v1/discover/tv/{id}/season/{n}` follow the group, and season requests use those
numbers, which match Sonarr's. Specials (season 0) can be requested whole
(`seasons: [0]`) or one episode at a time.

### Download status

Once Sonarr / Radarr have a request, `GET /v1/discover/{type}/{tmdbId}` and every item of
`GET /v1/discover/requests` carry `download`:

```
{ "state": "downloading", "progress": 0.45, "timeLeftSec": 720,
  "message": "…the arr app's own words for a problem…",
  "have": 7, "wanted": 12, "upcoming": 1, "nextAir": "2026-11-03T15:00:00Z",
  "lastSearch": "2026-10-26T06:06:16Z",
  "items":  [{ "label": "S01E08", "release": "…", "state": "downloading", "progress": 0.45, "timeLeftSec": 720 }],
  "groups": [{ "state": "downloading", "label": "Season 1 · 5 episodes", "count": 5, "progress": 0.4 }] }
```

`state` is the most important of: `failed` (the arr's reason in `message`), `attention`
(downloaded but cannot be imported), `stalled` (an arr warning, paused in the client, or
no bytes for 10 minutes), `downloading`, `importing`, `queued`, `notFound` (searched,
nothing grabbed; the arr keeps looking), `searching`, `waiting` (not aired / released
yet; `nextAir`), `complete`. `have` / `wanted` count requested, aired episodes. While
the arr app replaces a failed release on its own, `message` says "Trying another
release: the last one failed (…)". `groups` folds `items` by state for display.

The server builds it from each title's own queue (`/queue/details`, unpaged, not the
first 10 queue records `/queue` returns), episodes (`hasFile`, `airDateUtc`, `lastSearchTime`)
and history (`downloadFailed`), every 10 s while something moves and every 2 min
otherwise, and at once when a Sonarr / Radarr webhook arrives.

When `have` goes up between two passes (an import finished), the server asks the arr
app for the title's files (`/episodefile?seriesId=`, or the movie's `movieFile`), maps
them to its own paths (the webhook resolver below) and adds the ones the library lacks.
A requested title therefore shows up within seconds even with no webhook set up; titles
added in Sonarr / Radarr directly need the webhook (or a scan).

`episodes` lists every requested episode still missing with its own `state`, `progress`,
`timeLeftSec`, `message`, `airDate` (waiting) and `canSearch` / `canRetry`; the same
object is on each episode of `GET /v1/discover/tv/{id}/season/{n}` as `download`.

- `POST /v1/discover/requests/{id}/search` `{episodes?: ["S1E3"]}` → `{ok}`: search again
  for what is missing, or just those episodes (anyone).
- `POST /v1/discover/requests/{id}/replace` `{episodes?}` (admin) → `{ok}`: remove the
  failed / stalled / unimportable downloads (all, or those episodes') from the client,
  blocklist those releases and let the arr app pick another.

Guards, so a button never piles onto work already under way (the server enforces them
and returns `409` with the reason; `canSearch` / `canRetry` tell the apps when to show
the buttons):

- No search for an episode (or movie) that is in the download client, even one that
  only just stalled; one the arr app is searching for right now (its running
  `EpisodeSearch` / `SeasonSearch` / `SeriesSearch` / `MissingEpisodeSearch` /
  `MoviesSearch` commands); or one searched in the last 10 minutes.
- No replacing a stalled download until it has been stalled for 10 minutes (it often
  recovers); failed and unimportable downloads can be replaced at once.
- Asking again for a season that is already requested searches its missing episodes
  under the same rules.
- `searching` is shown only while a search runs or right after the request; searched
  without a result is `notFound`.

`GET /v1/discover/{type}/{tmdbId}/details` → the "More details" page for a Discover title,
in the shape of `/v1/items/{id}/details` (TMDB cast, keywords, companies, dates, links;
`backdrop` is an absolute image URL).

Sonarr: a whole-series request sets `monitorNewItems: all` (seasons that air later are
fetched too); a request for some seasons sets `none`. Test connection reports the arr
version and whether it is one Roxy is tested with (Sonarr v4; Radarr v5, v6).

A test bench with real Sonarr / Radarr and a fake indexer and download client is in
`deploy/dev/arrsim/`.

### Seasons and requests

A series detail carries `seasons: [{number, name, episodes, airDate, poster, have, known,
requested}]` (`have` = episodes with a file, `known` = files + placeholders). `POST
/v1/discover/request` takes `seasons: [n, …]` to ask for those only (empty = every season
the library lacks); a later request widens the existing one; seasons already complete are
refused. `GET /v1/discover/tv/{tmdbId}/season/{n}` → `{episodes: [{number, title,
overview, airDate, still, have}]}` marks which episodes the library holds, so a
half-complete season shows which half. Specials (TMDB season 0) appear last with
`special: true` and are requested one episode at a time: `POST /v1/discover/request`
`{type: "tv", tmdbId, episodes: ["S0E3"]}` (up to 12); the season-0 episode list carries
`requested` per episode. In Sonarr those episodes are monitored individually with an
EpisodeSearch. Sonarr gets per-season monitoring and a SeasonSearch per requested season (or
the seasons monitored on an already-added series). `requested_by` names the profile that
asked when the login is shared.

### Sonarr / Radarr settings (admin)

The arr connection settings, saved on the server (settings key `arr`, migration 0010); the
`ROXY_SONARR_*` / `ROXY_RADARR_*` variables seed it when nothing is saved.

- `GET /v1/settings/arr` → `{sonarr, radarr, requestsEnabled}` with API keys masked as `••••••••`.
- `PUT /v1/settings/arr` `{sonarr: {url, apiKey, rootFolder, qualityProfileId,
  animeRootFolder, animeQualityProfileId, oldAnimeQualityProfileId, animeSeriesType,
  tags, animeTags, seasonFolders, searchOnAdd, externalUrl}, radarr: {url, apiKey,
  rootFolder, qualityProfileId, animeRootFolder, animeQualityProfileId, tags,
  minimumAvailability, searchOnAdd, externalUrl}, pathMappings}`.
  - The body is applied **over** the saved settings: a field left out keeps its value
    (an older app that has never heard of a setting does not wipe it). Clear a value by
    sending it empty or `0`, not `null`. A masked key keeps the saved one.
  - `animeSeriesType`: `standard` (default; SxxEyy names, like the rest of a library) or
    `anime` (absolute numbering). `oldAnimeQualityProfileId`: see requests above; `0` =
    the anime profile.
  - Saving rebuilds the clients and sends pending requests (also retried every 10 min).
- `POST /v1/settings/arr/test` `{kind, url, apiKey}` → `{version, appName, rootFolders,
  qualityProfiles, tags}` so the app can offer dropdowns.
- `DELETE /v1/discover/requests/{id}` → 204; the requester or an admin only.

## Webhooks (Sonarr / Radarr → Roxy)

`POST /v1/webhooks/sonarr` and `/v1/webhooks/radarr` (also `/v1/ingest/…`) take the arr
apps' Webhook connection payloads (`docs/api/arr-webhooks.md`). Auth: an API key as
`X-Api-Key` or `?api_key=`, or an admin token. Handled: `Download` (deleted files
dropped, new file ingested), `Rename` (old source dropped, new path ingested),
`EpisodeFileDelete` / `MovieFileDelete`, `SeriesDelete` / `MovieDelete` (everything
under the folder), `Test`; the rest are logged. Paths are the arr app's own: an explicit
`pathMappings: [{from, to}]` on the arr settings document is applied first, else the
longest tail of the path that exists under a library root (or, for removals, that the
library knows) wins. Every notification lands in `GET /v1/webhooks/events?limit=`
(admin) with its outcome. API keys: `GET/POST /v1/auth/keys`, `DELETE
/v1/auth/keys/{id}` (admin; the token is returned once on create). Both lists are
always arrays (`{"keys": []}`, `{"events": []}`), never `null`. Sonarr and Radarr send
a `Test` both on the connection's Test button and when it is saved, so two test events
after setting one up are expected.

## Legal

The Terms of Use and Privacy text live on the server so every device shows the same
version. `GET /v1/legal/terms` and `/v1/legal/privacy` (no auth) → `{doc, title,
version, body}`; `GET /v1/legal/status` → `{version, accepted, acceptedVersion}` for
the login; `POST /v1/legal/accept {version}` records acceptance (409 on a stale
version). The app requires the "I agree" checkbox on the sign-in screen and records the
acceptance after login; Account → About links both documents.

## App updates

Self-hosted apps have no store, so Roxy Server is the update channel. After a build,
`apps/android/tools/publish-apk.ps1 -Notes "..."` copies the APK into `<data>/apps/tv/` and
writes `latest.json` keyed by applicationId (debug and release tracked separately).

- `GET /v1/apps/tv/latest?package=<applicationId>` → `{versionCode, versionName, notes,
  publishedAt, size, url}`; 404 when nothing is published for that package.
- `GET /v1/apps/tv/download?package=<applicationId>` → the APK (Bearer or `api_key`).

The app checks once per launch (and at most every 6 h) after the session is validated,
compares `versionCode` with its own, and shows a bar above the bottom navigation:
Update downloads the APK to its cache and hands it to the system installer (Android asks
once to allow installs from Roxy); the close button hides that build until a newer one
appears.

## Not yet (see ROADMAP)

Playback decision (`/v1/items/{id}/playback`), device-code login, websocket events,
HLS transcoding for browsers and Cast, subtitle burn-in.
