# Music (v1)

The music part of the native API (`native.md` has the rules that hold everywhere: auth,
JSON, camelCase, ISO-8601 times, milliseconds for durations). Every route here needs a
signed-in user; plays and favourites belong to the profile named by `X-Roxy-Profile`.

## How music is read

- **Folders.** An admin adds music folders (Library settings, or `ROXY_MUSIC_ROOTS`,
  comma separated, on first start). They are separate from the video libraries.
- **A song is a file.** Every audio file (`mp3 flac m4a aac ogg oga opus wav aiff aif wma
  ape wv dsf mka`) is one song, named by its **tags**: where the file sits and what it is
  called do not matter, except that a file without an artist tag takes the name of the
  top folder it is in, and one without a title takes its file name.
- **Artists.** An artist tag may name several artists (`A feat. B`, `A, B & C`, `A; B`,
  `A • B`). Each is credited, the main one first, and the song is listed on the page of
  each. A name is kept whole when the library knows it as one artist (it is the name of
  an artist folder, or some file is tagged with it alone), so "Tyler, The Creator" is
  not two artists. When one of the credited artists is the one whose folder the file is
  in, that artist leads.
- **Albums.** An album is its album artist plus its title. It is **listed once two of its
  songs are there**: a library of single songs is not a wall of one-song albums. A lone
  song still says which album it is from (`album`, `albumId`), and that album can be
  opened.
- **The same song.** A song is known by its main artist and title. A file that is moved,
  renamed or replaced by another copy of the same song (an MP3 by a FLAC) keeps the
  song's id, play counts and favourite mark. A file that is gone is hidden, and its row
  kept for 30 days in case it comes back.
- **An empty folder changes nothing.** A music folder that cannot be read, or holds no
  audio at all where there were songs, is what an unmounted drive looks like: its songs
  stay listed and the scan says so (`leftAlone`).
- **Pictures.** A song's picture is the one in the file; without one, its album's. An
  album's is `cover.*`, `folder.*`, `front.*` or `album.*` in a folder that holds that
  album and nothing else, else the picture most of its songs carry. An artist's is
  `artist.*` in their folder, else the picture of their newest song. Pictures are kept
  once each, under the hash of their bytes.
- **Lyrics.** `Song.lrc` (synced) or `Song.txt` beside `Song.mp3`, else the lyrics the
  audio file carries.
- Folders are looked at again every ten minutes (only new and changed files are read),
  at start, and on request.

## Objects

**Track**

| Field | Meaning |
|---|---|
| `id` | stable for the song (see "The same song") |
| `title`, `artist` | the artist as tagged (`"Joost, Ski Aggu"`) |
| `artistId`, `artists` | the main artist, and everyone credited as `[{id, name}]`, the main one first |
| `albumId`, `album`, `albumArtist` | absent when the file names no album |
| `disc`, `track`, `year` | absent when unknown |
| `durationMs`, `size` | |
| `container`, `codec`, `bitrate` (bits/s), `sampleRate`, `bitDepth`, `channels`, `lossless` | how the file is encoded |
| `art` | hash of the picture to show (the file's own, else its album's); absent for none |
| `lyrics` | `"lrc"`, `"txt"` or `"embedded"`; absent when there are none |
| `gainTrack`, `gainAlbum` | ReplayGain in dB, as tagged; absent when not tagged |
| `favorite`, `playCount`, `lastPlayedAt` | the profile's |
| `addedAt` | when the song came in (the file's time for a library read for the first time) |

**Album**: `id`, `title`, `artistId`, `artist`, `year`, `art`, `trackCount`, `durationMs`,
`addedAt`, `favorite`.

**Artist**: `id`, `name`, `art`, `trackCount` (songs credited on), `albumCount` (listed
albums), `favorite`.

## Reading

| Method | Path | Result |
|---|---|---|
| GET | `/v1/music` | `{counts: {tracks, albums, artists}, scanning, recentlyAdded, recentlyPlayed, mostPlayed}`. `counts.tracks` is 0 on a server with no music: a client then shows no Music section. `recentlyAdded` is a list of `{album}` or `{track}` (an album that came in is one entry, not one per song); the other two are tracks. |
| GET | `/v1/music/tracks` | `{total, tracks}`. `sort`: `title` (default), `artist`, `album` (disc and track order), `added`, `plays`, `recent` (last played; only played songs), `favorited`. Filters: `q`, `artist` (everything the artist is credited on), `album`, `favorite=1`, `played=1`, `ids=a,b` (those songs, in that order when no sort is given). `limit` (default 200, at most 1000), `offset`. |
| GET | `/v1/music/tracks/{id}` | a track |
| GET | `/v1/music/artists` | `{total, artists}` by name: the artists who lead at least one song. `guests=1` adds those who only appear on others' songs. `q`, `favorite=1`, `limit`, `offset`. |
| GET | `/v1/music/artists/{id}` | `{artist, tracks, albums}`: every song the artist is credited on (most played first, then by title) and their albums, newest first |
| GET | `/v1/music/albums` | `{total, albums}`. `sort`: `title` (default), `added`, `year`, `artist`. `q`, `artist`, `favorite=1`, `singles=1` (also albums only one song of which is there), `limit`, `offset`. |
| GET | `/v1/music/albums/{id}` | `{album, tracks}` in disc and track order |
| GET | `/v1/music/search?q=` | `{artists, albums, tracks}`, a few of each. Every word must be found; case and accents do not matter. |
| GET | `/v1/music/tracks/{id}/lyrics` | `{source, synced, lines: [{ms, text}]}`; 404 when the song has none. Synced lines are in time order (`ms` is when a line starts); unsynced lines all have `ms` 0. |
| GET | `/v1/music/art/{hash}` | the picture. `?w=<px>` resizes (never enlarges) to one of the fixed widths posters use. The address names the picture's bytes, so it is sent `immutable`: keep it for good. |

## Playing

| Method | Path | Result |
|---|---|---|
| GET, HEAD | `/v1/music/tracks/{id}/stream` | the audio file as it is, with HTTP ranges. Nothing is converted: a FLAC is sent as the FLAC it is. Roxy Web's `<audio>` asks with the session cookie. |
| POST | `/v1/music/tracks/{id}/played` | `{playCount}`. Counts one listen. A player sends it **once per play, when half the song or four minutes has been heard** (time skipped over is not heard). A second report for the same song within moments is the same listen and is not counted. |
| POST, DELETE | `/v1/music/favorites/{kind}/{id}` | 204. `kind` is `track`, `album` or `artist`. |

## Folders (admin)

| Method | Path | Result |
|---|---|---|
| GET | `/v1/music/folders` | `{folders: [{id, name, root, reachable, tracks}], scan}` |
| POST | `/v1/music/folders` | `{path, name}` -> the folder; a scan starts. Refused (400) for a path that is not a readable folder, is already a music folder, or is inside or around one. |
| DELETE | `/v1/music/folders/{id}` | 204. The folder's songs, with their play counts and favourite marks, are forgotten; the files stay. |
| POST | `/v1/music/scan` | `{force}` -> 202. `force` reads every file again, not only new and changed ones. |
| GET | `/v1/music/scan` | `{scanning, startedAt, finishedAt, done, total, folders, error}`. `folders` is what the last pass did in each: `{folderId, name, files, added, updated, moved, missing, removed, failed, leftAlone, problems}`. |

`GET /v1/library/browse` (the folder picker) also says how many audio files a folder
holds (`audio`), beside `videos`.

## Not there yet

Conversion for players that cannot play a format (the stream is always the file itself),
playlists, loudness measured by the server (only tagged ReplayGain is passed on), and
media tokens for players that cannot send a header or a cookie.
