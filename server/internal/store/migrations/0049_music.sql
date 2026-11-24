-- Music. A song is not a film or an episode: it has no place in `items` (whose types are
-- fixed, and which every video query reads). Music has tables of its own, filled from
-- the files' tags, and an older build simply does not look at them.

-- The folders music is read from (an admin adds them; they are not video libraries).
CREATE TABLE music_folders (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    root       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

-- An artist is whoever the tags credit. The id is made from the folded name (name_key),
-- so an artist whose last song left and came back is the same artist, with the same
-- address and the same favourite mark.
CREATE TABLE music_artists (
    id         TEXT PRIMARY KEY,
    name_key   TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    sort_name  TEXT NOT NULL,
    art        TEXT NOT NULL DEFAULT '',   -- picture (content hash); '' = none
    created_at TEXT NOT NULL
);

-- An album is what its tracks say it is: album artist + album title (album_key). The
-- counts are of the files that are there now.
CREATE TABLE music_albums (
    id          TEXT PRIMARY KEY,
    album_key   TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    sort_title  TEXT NOT NULL,
    artist_id   TEXT NOT NULL REFERENCES music_artists(id),
    artist      TEXT NOT NULL,
    year        INTEGER NOT NULL DEFAULT 0,
    art         TEXT NOT NULL DEFAULT '',
    track_count INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    added_at    TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX music_albums_artist ON music_albums(artist_id);

-- One row per audio file. `identity` says which song it is (artist + title, folded): a
-- file that is moved, renamed or replaced by a better copy of the same song takes over
-- the row of the one that left, with its id, its play count and its favourite mark.
-- A file that is gone is kept, hidden (missing_since), for a while for the same reason.
CREATE TABLE music_tracks (
    id            TEXT PRIMARY KEY,
    folder_id     INTEGER NOT NULL REFERENCES music_folders(id) ON DELETE CASCADE,
    path          TEXT NOT NULL UNIQUE,
    size          INTEGER NOT NULL,
    mtime         INTEGER NOT NULL,            -- unix nanoseconds
    identity      TEXT NOT NULL,
    title         TEXT NOT NULL,
    sort_title    TEXT NOT NULL,
    artist        TEXT NOT NULL,               -- as tagged ("A feat. B")
    artist_id     TEXT NOT NULL REFERENCES music_artists(id),
    album_id      TEXT REFERENCES music_albums(id) ON DELETE SET NULL,
    album         TEXT NOT NULL DEFAULT '',    -- as tagged
    album_artist  TEXT NOT NULL DEFAULT '',    -- the album's artist, one name
    disc          INTEGER NOT NULL DEFAULT 0,
    track         INTEGER NOT NULL DEFAULT 0,
    year          INTEGER NOT NULL DEFAULT 0,
    genres        TEXT NOT NULL DEFAULT '',    -- JSON list
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    container     TEXT NOT NULL DEFAULT '',
    codec         TEXT NOT NULL DEFAULT '',
    bitrate       INTEGER NOT NULL DEFAULT 0,  -- bits per second
    sample_rate   INTEGER NOT NULL DEFAULT 0,
    bit_depth     INTEGER NOT NULL DEFAULT 0,
    channels      INTEGER NOT NULL DEFAULT 0,
    art           TEXT NOT NULL DEFAULT '',    -- the file's own picture (content hash)
    lyrics        TEXT NOT NULL DEFAULT '',    -- where lyrics are: 'lrc' | 'txt' | 'embedded' | ''
    lyrics_text   TEXT NOT NULL DEFAULT '',    -- the lyrics the file itself carries
    mb_recording  TEXT NOT NULL DEFAULT '',
    mb_album      TEXT NOT NULL DEFAULT '',
    gain_track    REAL,                        -- ReplayGain, dB, as tagged
    peak_track    REAL,
    gain_album    REAL,
    peak_album    REAL,
    search        TEXT NOT NULL DEFAULT '',    -- folded title, artist and album
    missing_since TEXT,
    added_at      TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX music_tracks_album    ON music_tracks(album_id);
CREATE INDEX music_tracks_artist   ON music_tracks(artist_id);
CREATE INDEX music_tracks_identity ON music_tracks(identity);
CREATE INDEX music_tracks_added    ON music_tracks(added_at DESC);

-- Everyone a track credits, in order (the first is music_tracks.artist_id): a song is
-- listed on the page of each of them.
CREATE TABLE music_track_artists (
    track_id  TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    artist_id TEXT NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    PRIMARY KEY (track_id, artist_id)
);
CREATE INDEX music_track_artists_artist ON music_track_artists(artist_id);

-- What a profile did with a song, an album or an artist: plays and the favourite mark.
-- Its own table, not user_item_data: nothing here is "watched" or resumed, and the video
-- side's lists (continue watching, history) must never meet a song.
CREATE TABLE music_user_data (
    scope_id       TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('track', 'album', 'artist')),
    ref_id         TEXT NOT NULL,
    play_count     INTEGER NOT NULL DEFAULT 0,
    last_played_at TEXT,
    is_favorite    INTEGER NOT NULL DEFAULT 0,
    favorited_at   TEXT,
    PRIMARY KEY (scope_id, kind, ref_id)
);
CREATE INDEX music_user_data_ref ON music_user_data(kind, ref_id);
