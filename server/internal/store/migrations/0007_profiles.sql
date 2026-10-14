-- Profiles: several viewers on one login (Netflix-style). Watch state, favourites and
-- the home screen are per profile, so user_item_data is re-keyed by profile id
-- ("scope"). Every existing user gets a default profile named after them that inherits
-- their history; the Jellyfin-compat surface and clients that send no profile use it.
CREATE TABLE profiles (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    avatar_url  TEXT,          -- where the picture came from (AniList character image)
    avatar_path TEXT,          -- cached copy on disk
    avatar_name TEXT,          -- the character's name, for the picker
    is_default  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);
CREATE INDEX profiles_user ON profiles(user_id);

INSERT INTO profiles (id, user_id, name, is_default, created_at)
SELECT lower(hex(randomblob(16))), id, name, 1, created_at FROM users;

CREATE TABLE user_item_data_new (
    scope_id              TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    item_guid             TEXT NOT NULL,
    position_ms           INTEGER NOT NULL DEFAULT 0,
    play_count            INTEGER NOT NULL DEFAULT 0,
    played                INTEGER NOT NULL DEFAULT 0,
    is_favorite           INTEGER NOT NULL DEFAULT 0,
    last_played_at        TEXT,
    audio_stream_index    INTEGER,
    subtitle_stream_index INTEGER,
    PRIMARY KEY (scope_id, item_guid)
);
INSERT INTO user_item_data_new (scope_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index)
SELECT p.id, u.item_guid, u.position_ms, u.play_count, u.played, u.is_favorite, u.last_played_at, u.audio_stream_index, u.subtitle_stream_index
FROM user_item_data u JOIN profiles p ON p.user_id = u.user_id AND p.is_default = 1;
DROP TABLE user_item_data;
ALTER TABLE user_item_data_new RENAME TO user_item_data;
CREATE INDEX user_item_data_guid ON user_item_data(item_guid);
