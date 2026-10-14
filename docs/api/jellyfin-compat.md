# Jellyfin-compatible API subset

What Roxy Server must implement so request app, Sonarr, Radarr and the Roxy TV app v1 work
unchanged. Shapes follow Jellyfin 12.x. Sources and field-level evidence are in
`docs/research/notes/arr_pipeline_contract.md` and `build_vs_fork.md`.

## Authentication

Accept on every request:

```
Authorization: MediaBrowser Client="…", Device="…", DeviceId="…", Version="…", Token="…"
X-MediaBrowser-Token: <token>          (Sonarr/Radarr also send this)
X-Emby-Token: <token>                  (accept for safety)
```

A token resolves to either a device (user session) or an API key (no user, admin scope).

## request app

| Method | Path | Must return / notes |
|---|---|---|
| POST | `/Users/AuthenticateByName` | body `{Username, Pw}` -> `{User:{Id, Name, ServerId, Policy:{IsAdministrator}, PrimaryImageTag?}, AccessToken, ServerId}`; 401 on bad creds |
| POST | `/QuickConnect/Initiate` | `{Code, Secret, DateAdded, Authenticated:false}` |
| GET | `/QuickConnect/Connect?secret=` | `{Authenticated: bool, ...}` |
| POST | `/Users/AuthenticateWithQuickConnect` | body `{Secret}` -> same shape as AuthenticateByName |
| GET | `/System/Info` | `{Id, ServerName, Version, OperatingSystem, ...}` (admin) |
| GET | `/System/Info/Public` | `{Id, ServerName, Version, ...}` |
| GET | `/Users` | `[{Id, Name, ServerId, Policy:{IsAdministrator, IsDisabled}, PrimaryImageTag?}]` |
| GET | `/Users/{id}`, `/Users/Me` | same user shape |
| GET | `/Library/MediaFolders` | `{Items:[{Id, Name, CollectionType ("movies"/"tvshows"), Type:"CollectionFolder"}]}` |
| GET | `/Users/{id}/Views` | fallback for the above |
| GET | `/Items?SortBy=SortName&SortOrder=Ascending&IncludeItemTypes=Series,Movie,Others&Recursive=true&StartIndex=&Limit=&ParentId=&collapseBoxSetItems=false` | `{Items:[...], TotalRecordCount}` paged |
| GET | `/Items/Latest?userId=&Limit=12&ParentId=` | array of items; episodes must carry `SeriesId`, `SeasonId` |
| GET | `/Items?ids={id,...}&fields=ProviderIds,MediaSources,Width,Height,IsHD,DateCreated` | item detail (request app does not use `/Items/{id}`) |
| GET | `/Shows/{seriesId}/Seasons` | `{Items:[{Id, Name, IndexNumber (= TMDb season number), SeriesId}]}` |
| GET | `/Shows/{seriesId}/Episodes?seasonId=&fields=MediaSources` | `{Items:[episode...]}` |
| POST | `/Auth/Keys?App=request app` | 204; creates API key |
| GET | `/Auth/Keys` | `{Items:[{AccessToken, AppName, DateCreated}]}` |

Item fields request app reads: `Id`, `Name`, `Type` (`Movie|Series|Season|Episode`),
`LocationType`, `DateCreated` (ISO-8601), `SeriesId`, `SeasonId`, `IndexNumber`,
`IndexNumberEnd`, `ProviderIds` (`Tmdb`, `Tvdb`, `Imdb`, `AniDB`),
`MediaSources[].MediaStreams[]` with `Type:"Video"` and `Width` (**Width > 2000 is 4K**).

Availability rules to satisfy: seasons are exposed with TMDb numbering; `/Items/Latest`
reflects new files within seconds of ingest (request app polls every 5 minutes); a season is
AVAILABLE only when the episode count equals TMDb's.

## Sonarr / Radarr (Emby/Jellyfin connect)

| Method | Path | Notes |
|---|---|---|
| GET | `/System/Configuration` | any 200 JSON; used as the connection test |
| POST | `/Notifications/Admin` | `{Name, Description}` -> 204 |
| GET | `/Items?recursive=true&includeItemTypes=Series&fields=Path,ProviderIds&years=` | series lookup by TVDb/IMDb/TMDb; return `Path` |
| POST | `/Library/Media/Updated` | `{Updates:[{Path, UpdateType}]}` -> 204; enqueue ingest for `Path` |
| POST | `/Library/Refresh` | 204; full reconciliation (admin) |

Paths arrive as Sonarr/Radarr see them; Roxy mounts `/data` identically or applies a
prefix map.

## Roxy TV app v1 (playback)

| Method | Path | Notes |
|---|---|---|
| POST | `/Items/{id}/PlaybackInfo?userId=` | body `{DeviceProfile, MaxStreamingBitrate, StartTimeTicks, AudioStreamIndex, SubtitleStreamIndex, MediaSourceId, EnableDirectPlay, EnableDirectStream, EnableTranscoding, AlwaysBurnInSubtitleWhenTranscoding}` -> `PlaybackInfoResponse{MediaSources[{Id, Container, SupportsDirectPlay, SupportsDirectStream, SupportsTranscoding, TranscodingUrl?, TranscodingSubProtocol, MediaStreams[{..., DeliveryMethod, DeliveryUrl, IsExternal}], MediaAttachments[{Index, FileName, MimeType, DeliveryUrl}], TranscodeReasons[]}], PlaySessionId}` |
| GET | `/Videos/{id}/stream?mediaSourceId=&static=true&api_key=` | direct play with HTTP ranges |
| GET | `/Videos/{id}/{mediaSourceId}/Attachments/{index}` | font bytes |
| GET | `/Videos/{id}/{mediaSourceId}/Subtitles/{index}/Stream.{ext}` and `/Subtitles/{index}/0/Stream.{ext}` | sidecar text track, byte-for-byte for ASS/SSA |
| GET | `/videos/{id}/master.m3u8?...` (Phase 3) | HLS transcode |
| POST | `/Sessions/Playing`, `/Sessions/Playing/Progress`, `/Sessions/Playing/Stopped`, `/Sessions/Playing/Ping` | playback reporting |
| GET | `/Users/{id}/Items`, `/Users/{id}/Items/Resume`, `/Shows/NextUp`, `/Users/{id}/Items/Latest` | browsing |
| GET | `/Items/{id}/Images/{type}` | artwork |
| POST/DELETE | `/Users/{id}/PlayedItems/{id}`, `/Users/{id}/FavoriteItems/{id}` | user data |
| WS | `/socket?api_key=&deviceId=` | `LibraryChanged`, `UserDataChanged`, `KeepAlive` |

Decision mapping for ASS on the compat surface: a `SubtitleProfile{Format:"ass", Method:"External"}`
makes Roxy return `DeliveryMethod:"External"` with a `DeliveryUrl`; without it, Roxy
reports `Encode` (burn-in) and, in Phase 2 with no transcoder, `SupportsTranscoding:false`.

## Testing

Capture real traffic from request app 3.5, Sonarr 4.0.x, Radarr 6.x and the TV app against
Jellyfin 12.2 (mitmproxy), store as fixtures under `server/internal/api/compat/testdata/`,
and replay them against Roxy in CI. Re-capture on each request app release.
