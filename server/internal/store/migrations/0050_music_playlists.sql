-- Playlists: a profile's own lists of songs, in an order it chooses. A song may be in a
-- list more than once, so an entry has an id of its own (what is removed or moved is an
-- entry, not a song). Entries name the track row, which stays the same when the song's
-- file is moved or replaced; a song whose file is gone keeps its place and is not shown
-- until the file is back.
CREATE TABLE music_playlists (
    id         TEXT PRIMARY KEY,
    scope_id   TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX music_playlists_scope ON music_playlists(scope_id);

CREATE TABLE music_playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES music_playlists(id) ON DELETE CASCADE,
    track_id    TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    added_at    TEXT NOT NULL
);
CREATE INDEX music_playlist_tracks_list  ON music_playlist_tracks(playlist_id, position);
CREATE INDEX music_playlist_tracks_track ON music_playlist_tracks(track_id);
