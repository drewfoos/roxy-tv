# Sonarr / Radarr webhook contract (ingest input)

Roxy Server exposes `POST /v1/ingest/sonarr` and `POST /v1/ingest/radarr` (also reachable
at `/webhooks/sonarr` and `/webhooks/radarr`), configured in each app as a generic
**Webhook** connection with an API key in a header. Payload field inventory from the
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
    "sourcePath": "/data/torrents/tv/..."
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
  "deletedFiles": [ ... ],
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
| `SeriesDelete` / `MovieDelete` | remove item tree; user data retained by GUID |
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
