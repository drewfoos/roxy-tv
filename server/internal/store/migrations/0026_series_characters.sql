-- Characters of each series for the profile editor's "From your library" avatars, kept on
-- the server instead of asking AniList for forty shows every time the editor opens. Filled
-- a few series at a time by a background worker (AniList for anime, TMDB's cast otherwise).
CREATE TABLE series_characters (
    series_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    pos          INTEGER NOT NULL,          -- order in the cast, leads first
    source       TEXT NOT NULL,             -- anilist | tmdb
    char_id      INTEGER NOT NULL,          -- AniList character id; minus the TMDB person id
    name         TEXT NOT NULL,
    image        TEXT NOT NULL,
    series_title TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (series_id, pos)
);

-- Which series were looked up, and whether the lookup worked (a failed one is tried again
-- later, a few times at most).
CREATE TABLE series_characters_state (
    series_id  TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    fetched_at TEXT NOT NULL,
    ok         INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 1
);
