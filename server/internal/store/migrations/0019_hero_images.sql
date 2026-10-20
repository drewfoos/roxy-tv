-- A fifth image type, "hero": a textless poster for the phone's Home banner. SQLite
-- cannot alter a CHECK constraint, so the table is rebuilt with the wider list.
CREATE TABLE item_images_new (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('poster', 'backdrop', 'logo', 'thumb', 'hero')),
    path       TEXT NOT NULL,
    width      INTEGER,
    height     INTEGER,
    source_url TEXT,
    fetched_at TEXT NOT NULL,
    locked     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, type)
);
INSERT INTO item_images_new (item_id, type, path, width, height, source_url, fetched_at, locked)
    SELECT item_id, type, path, width, height, source_url, fetched_at, locked FROM item_images;
DROP TABLE item_images;
ALTER TABLE item_images_new RENAME TO item_images;
