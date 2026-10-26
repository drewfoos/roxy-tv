-- Three user levels: admin (everything), requester (requests go straight to Sonarr /
-- Radarr), basic (requests wait for an admin to approve). Existing admins stay admins;
-- every other existing account becomes a requester, which is how they worked before.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'requester';
UPDATE users SET role = CASE WHEN is_admin = 1 THEN 'admin' ELSE 'requester' END;

-- Requests gain two states: 'approval' (a basic user's request waiting for an admin) and
-- 'declined'. SQLite can't change a CHECK in place, so the table is rebuilt (nothing
-- references it). Columns and rows are copied as they are.
CREATE TABLE requests_new (
    id           TEXT PRIMARY KEY,
    media_type   TEXT NOT NULL CHECK (media_type IN ('tv', 'movie')),
    tmdb_id      INTEGER NOT NULL,
    title        TEXT NOT NULL,
    year         INTEGER,
    poster_path  TEXT,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'approval', 'declined')),
    arr_id       INTEGER,
    error        TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    seasons      TEXT,
    requested_by TEXT,
    episodes     TEXT,
    UNIQUE (media_type, tmdb_id)
);
INSERT INTO requests_new (id, media_type, tmdb_id, title, year, poster_path, user_id, status, arr_id, error, created_at, updated_at, seasons, requested_by, episodes)
    SELECT id, media_type, tmdb_id, title, year, poster_path, user_id, status, arr_id, error, created_at, updated_at, seasons, requested_by, episodes FROM requests;
DROP TABLE requests;
ALTER TABLE requests_new RENAME TO requests;
