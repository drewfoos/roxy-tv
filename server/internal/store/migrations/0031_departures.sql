-- Media files that left the library recently, kept so watch history can follow a file
-- that turns up again elsewhere: a renamed or moved folder, a file moved to another
-- library, a remux under the same name, or the same film or episode fetched again. Each
-- row is matched against files that arrive (see store/moves.go) and dropped once settled
-- or after 30 days.
CREATE TABLE departures (
    id          INTEGER PRIMARY KEY,
    path        TEXT NOT NULL,
    ext         TEXT NOT NULL,              -- lower case, with the dot
    stem        TEXT NOT NULL,              -- lower case file name without extension
    size        INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    chain       TEXT NOT NULL,              -- JSON [{id,guid,type,title}]: the item, then its parents
    top_ids     TEXT NOT NULL DEFAULT '{}', -- JSON provider ids of the top item (movie or series)
    season      INTEGER,                    -- an episode's season and episode number
    episode     INTEGER,
    departed_at TEXT NOT NULL
);
CREATE INDEX departures_size ON departures(size);

-- An item replaced by another when its file moved: clients holding the old id (a phone's
-- downloads, a bookmark) are sent to the new one.
CREATE TABLE item_redirects (
    old_id     TEXT PRIMARY KEY,
    new_id     TEXT NOT NULL,
    created_at TEXT NOT NULL
);
