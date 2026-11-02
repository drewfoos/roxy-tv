-- When the metadata service last looked a title up, matched or not (NULL = never). A
-- restart then skips titles it already tried: episodes no provider could fill, or a
-- title nothing matched, are tried again only when something new arrives (or after the
-- refresh interval), not on every start.
ALTER TABLE items ADD COLUMN metadata_attempted_at TEXT;
