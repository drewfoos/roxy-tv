-- Roxy Server schema v1. See docs/ARCHITECTURE.md section 4.2.
PRAGMA foreign_keys = ON;

CREATE TABLE libraries (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('movies', 'series')),
    root        TEXT    NOT NULL UNIQUE,
    anime_mode  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL
);

CREATE TABLE items (
    id             TEXT PRIMARY KEY,
    guid           TEXT NOT NULL UNIQUE,
    library_id     INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    type           TEXT NOT NULL CHECK (type IN ('movie', 'series', 'season', 'episode')),
    parent_id      TEXT REFERENCES items(id) ON DELETE CASCADE,
    title          TEXT NOT NULL,
    sort_title     TEXT,
    year           INTEGER,
    overview       TEXT,
    premiere_date  TEXT,
    index_aired    INTEGER,
    season_aired   INTEGER,
    index_absolute INTEGER,
    index_anidb    INTEGER,
    season_anidb   INTEGER,
    runtime_ms     INTEGER,
    date_created   TEXT NOT NULL,
    date_modified  TEXT NOT NULL
);
CREATE INDEX items_parent       ON items(parent_id);
CREATE INDEX items_library_type ON items(library_id, type);
CREATE INDEX items_created      ON items(date_created DESC);
CREATE INDEX items_type_created ON items(type, date_created DESC);

CREATE TABLE item_provider_ids (
    item_id  TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (item_id, provider)
);
CREATE INDEX item_provider_lookup ON item_provider_ids(provider, value);

CREATE TABLE media_sources (
    id            TEXT PRIMARY KEY,
    item_id       TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    name          TEXT,
    container     TEXT,
    size          INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    bitrate       INTEGER,
    version_label TEXT,
    is_primary    INTEGER NOT NULL DEFAULT 1,
    probed_at     TEXT,
    probe_json    TEXT
);
CREATE INDEX media_sources_item ON media_sources(item_id);

CREATE TABLE media_parts (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    part_index  INTEGER NOT NULL DEFAULT 0,
    path        TEXT NOT NULL UNIQUE,
    size        INTEGER NOT NULL DEFAULT 0,
    mtime       TEXT NOT NULL,
    duration_ms INTEGER
);
CREATE INDEX media_parts_source ON media_parts(source_id);

CREATE TABLE media_streams (
    source_id           TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    stream_index        INTEGER NOT NULL,
    type                TEXT NOT NULL CHECK (type IN ('video', 'audio', 'subtitle')),
    codec               TEXT NOT NULL,
    profile             TEXT,
    level               INTEGER,
    bit_depth           INTEGER,
    pix_fmt             TEXT,
    width               INTEGER,
    height              INTEGER,
    fps                 REAL,
    color_primaries     TEXT,
    color_transfer      TEXT,
    color_space         TEXT,
    range_type          TEXT,
    dv_profile          INTEGER,
    channels            INTEGER,
    channel_layout      TEXT,
    sample_rate         INTEGER,
    bitrate             INTEGER,
    language            TEXT,
    title               TEXT,
    is_default          INTEGER NOT NULL DEFAULT 0,
    is_forced           INTEGER NOT NULL DEFAULT 0,
    is_hearing_impaired INTEGER NOT NULL DEFAULT 0,
    is_external         INTEGER NOT NULL DEFAULT 0,
    external_path       TEXT,
    is_text_format      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (source_id, stream_index)
);

CREATE TABLE attachments (
    source_id      TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    stream_index   INTEGER NOT NULL,
    filename       TEXT NOT NULL,
    mime           TEXT,
    extracted_path TEXT,
    extracted_at   TEXT,
    PRIMARY KEY (source_id, stream_index)
);

CREATE TABLE users (
    id                       TEXT PRIMARY KEY,
    name                     TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash            TEXT,
    is_admin                 INTEGER NOT NULL DEFAULT 0,
    is_disabled              INTEGER NOT NULL DEFAULT 0,
    max_parental_rating      INTEGER,
    remote_bitrate_cap       INTEGER,
    subtitle_mode            TEXT NOT NULL DEFAULT 'default',
    subtitle_language        TEXT,
    audio_language           TEXT,
    remember_track_selection INTEGER NOT NULL DEFAULT 1,
    created_at               TEXT NOT NULL
);

CREATE TABLE user_library_access (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);

CREATE TABLE devices (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT,
    client         TEXT,
    client_version TEXT,
    device_id      TEXT,
    token_hash     TEXT NOT NULL UNIQUE,
    last_seen      TEXT,
    created_at     TEXT NOT NULL
);

CREATE TABLE api_keys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE user_item_data (
    user_id               TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_guid             TEXT NOT NULL,
    position_ms           INTEGER NOT NULL DEFAULT 0,
    play_count            INTEGER NOT NULL DEFAULT 0,
    played                INTEGER NOT NULL DEFAULT 0,
    is_favorite           INTEGER NOT NULL DEFAULT 0,
    last_played_at        TEXT,
    audio_stream_index    INTEGER,
    subtitle_stream_index INTEGER,
    PRIMARY KEY (user_id, item_guid)
);

CREATE TABLE device_codes (
    code       TEXT PRIMARY KEY,
    secret     TEXT NOT NULL UNIQUE,
    device_id  TEXT,
    client     TEXT,
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL
);

CREATE TABLE ingest_events (
    id           INTEGER PRIMARY KEY,
    source       TEXT NOT NULL,
    event_type   TEXT,
    payload_json TEXT,
    received_at  TEXT NOT NULL,
    processed_at TEXT,
    error        TEXT
);

CREATE VIRTUAL TABLE items_fts USING fts5(title, content='items', content_rowid='rowid');
