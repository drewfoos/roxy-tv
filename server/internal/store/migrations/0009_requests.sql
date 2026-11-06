-- Discover requests: a title someone wants added. Sent to Sonarr or
-- Radarr when they are configured; otherwise kept as pending so the household can see
-- what was asked for. `available` is not stored: it is true whenever the library has an
-- item with the same TMDB id.
CREATE TABLE requests (
    id           TEXT PRIMARY KEY,
    media_type   TEXT NOT NULL CHECK (media_type IN ('tv', 'movie')),
    tmdb_id      INTEGER NOT NULL,
    title        TEXT NOT NULL,
    year         INTEGER,
    poster_path  TEXT,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
    arr_id       INTEGER,
    error        TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (media_type, tmdb_id)
);
