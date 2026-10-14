-- Provider metadata (docs/api/native.md "Metadata and images"). `overview` and
-- `premiere_date` already exist in v1; premiere_date is what the API calls `premiered`.
ALTER TABLE items ADD COLUMN tagline TEXT;
ALTER TABLE items ADD COLUMN genres TEXT;            -- JSON array of strings
ALTER TABLE items ADD COLUMN community_rating REAL;  -- 0..10
ALTER TABLE items ADD COLUMN official_rating TEXT;   -- "TV-14", "PG-13"
ALTER TABLE items ADD COLUMN status TEXT;            -- series: Continuing | Ended
ALTER TABLE items ADD COLUMN studio TEXT;
ALTER TABLE items ADD COLUMN metadata_refreshed_at TEXT;

-- One cached file per (item, image type), stored under data/cache/images/<item>/<type>.jpg.
CREATE TABLE item_images (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('poster', 'backdrop', 'logo', 'thumb')),
    path       TEXT NOT NULL,
    width      INTEGER,
    height     INTEGER,
    source_url TEXT,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (item_id, type)
);
