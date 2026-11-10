# Sonarr / Radarr webhook contract (ingest input)

Roxy Server exposes `POST /v1/webhooks/sonarr` and `POST /v1/webhooks/radarr` (also
reachable at `/v1/ingest/sonarr` and `/v1/ingest/radarr`), configured in each app as a
generic **Webhook** connection with an API key in a header.

## Setting it up

1. In Roxy (web): Account → Webhooks → **New key for Sonarr** (and for Radarr). The key is
   shown once; copy it.
2. In Sonarr: Settings → Connect → + → Webhook. URL `http://<roxy-host>:<port>/v1/webhooks/sonarr`
   (the page shows it with a copy button), method POST, triggers On Import, On Upgrade,
   On Rename, On Series Delete, On Episode File Delete. Add a header `X-Api-Key` with the
   key (or append `?api_key=<key>` to the URL). Same in Radarr with `/v1/webhooks/radarr`
   and the movie triggers.
3. Test, then Save. Both send a `Test` event, so Roxy's Recent notifications shows two.

Without a webhook, titles requested through Roxy still appear on their own (the request
tracker notices new files); anything added in Sonarr / Radarr directly then needs a scan. Payload field inventory from the
Sonarr/Radarr source as of September 2026; JSON casing is camelCase in practice (not
verified from serializer settings, so parse case-insensitively).

## Sonarr

`eventType`: `Test`, `Grab`, `Download` (import and upgrade), `Rename`, `SeriesAdd`,
`SeriesDelete`, `EpisodeFileDelete`, `Health`, `HealthRestored`, `ApplicationUpdate`,
`ManualInteractionRequired`.

Fields Roxy uses:

```jsonc
{
  "eventType": "Download",
  "isUpgrade": false,
  "series": {
    "id": 12, "title": "...", "titleSlug": "...", "path": "/data/media/tv/Title (2020) [tvdbid-123]",
    "tvdbId": 123, "tmdbId": 456, "imdbId": "tt...", "tvMazeId": 0,
    "malIds": [111], "aniListIds": [222],
    "type": "anime", "year": 2020
  },
  "episodes": [
    { "id": 1, "episodeNumber": 1, "seasonNumber": 1, "title": "...", "airDateUtc": "...", "tvdbId": 999 }
  ],
  "episodeFile": {
    "id": 5, "relativePath": "Season 01/Title (2020) - S01E01 - 001 - Ep [..]-Grp.mkv",
    "path": "/data/media/tv/.../Season 01/....mkv",
    "quality": "WEBDL-1080p", "qualityVersion": 1, "releaseGroup": "Grp", "sceneName": "...",
    "size": 1400000000, "languages": [{"id":8,"name":"Japanese"}],
    "mediaInfo": { "audioChannels": 2.0, "audioCodec": "FLAC", "audioLanguages": ["jpn"], "height": 1080, "width": 1920,
                   "subtitles": ["eng"], "videoCodec": "x265", "videoDynamicRangeType": "" },
    "sourcePath": "/data/downloads/tv/..."
  },
  "deletedFiles": [ { "id": 4, "path": "...", "relativePath": "..." } ],   // present on upgrades
  "renamedEpisodeFiles": [ { "previousPath": "...", "path": "...", "relativePath": "..." } ]  // Rename only
}
```

Notes:

- `WebhookEpisode` has **no absolute number**. Take it from the `- 001 -` token in the
  filename (TRaSH anime format) or from the anime-lists offset.
- `mediaInfo` lacks bit depth; Roxy always probes.
- `SeriesDelete` / `EpisodeFileDelete` carry the series and file ids and paths.
- `deletedFiles` has two shapes. On `Download` it is the list of files an upgrade
  replaced. On `SeriesDelete` (and Radarr's `MovieDelete`) it is `true` or `false`: whether
  the title's files were deleted with it. `false` means the title was only taken out of
  the app and its files are still on disk, so Roxy keeps it (the event is logged as
  "kept"). Any other shape is rejected as a bad payload.

## Radarr

`eventType`: `Test`, `Grab`, `Download`, `Rename`, `MovieAdded`, `MovieDelete`,
`MovieFileDelete`, `Health`, `HealthRestored`, `ApplicationUpdate`, `ManualInteractionRequired`.

```jsonc
{
  "eventType": "Download",
  "isUpgrade": false,
  "movie": { "id": 7, "title": "...", "year": 2019, "folderPath": "/data/media/movies/Title (2019) [tmdbid-789]",
             "tmdbId": 789, "imdbId": "tt...", "releaseDate": "...", "tags": [] },
  "movieFile": { "id": 3, "relativePath": "Title (2019) [..]-Grp.mkv", "path": "/data/media/movies/.../....mkv",
                 "quality": "Bluray-1080p", "qualityVersion": 1, "releaseGroup": "Grp", "sceneName": "...",
                 "size": 9000000000, "mediaInfo": { ... }, "sourcePath": "..." },
  "deletedFiles": [ ... ],   // a list on upgrades; true / false on MovieDelete
  "renamedMovieFiles": [ { "previousPath": "...", "path": "..." } ]
}
```

## Handling matrix

| Event | Action |
|---|---|
| `Test` | 200, log |
| `Grab` | optional: show "downloading" badge via `ingest_events`; no library change |
| `Download` | upsert series/movie by IDs; remove `deletedFiles[]` sources; add new source; probe; extract; metadata refresh; publish `LibraryChanged`; make visible in `/Items/Latest` |
| `Rename` | update `media_parts.path` from previous->new; no probe |
| `EpisodeFileDelete` / `MovieFileDelete` | remove source; keep item if other sources remain, else mark missing (keep user data by GUID) |
| `SeriesDelete` / `MovieDelete` | `deletedFiles: true`: remove the files that are gone from disk (user data retained by GUID). `deletedFiles: false`: nothing removed, the title stays |
| `SeriesAdd` / `MovieAdded` | create item shell with IDs and pre-fetch metadata (shows as "requested/missing") |
| `Health*`, `ApplicationUpdate`, `ManualInteractionRequired` | 200, log |

## Reconciliation (hourly, and on `/Library/Refresh`)

`GET {sonarr}/api/v3/series`, `/api/v3/episodefile?seriesId=`, `{radarr}/api/v3/movie`,
`/api/v3/moviefile?movieId=` with `X-Api-Key`. Diff against `media_parts.path`; enqueue
ingest for missing files, mark gone files missing.

## Filesystem watcher

Watches `/data/media/**` (inotify on Linux, polling fallback), debounced 5 s, for:
Bazarr sidecars (`<base>.<lang>[.forced][.hi|.cc|.sdh][.tags].(srt|ass|ssa|sup|sub|idx)`),
manual drops (fallback filename parsing with TRaSH tokens `[tvdbid-N]`, `[tmdbid-N]`,
`[imdbid-ttN]`, `SxxEyy`, `- NNN -`).
