-- Placeholder items created from provider metadata for episodes/seasons the library
-- does not have (yet). They flip to real when a file with the same GUID is ingested.
ALTER TABLE items ADD COLUMN is_virtual INTEGER NOT NULL DEFAULT 0;
CREATE INDEX items_virtual ON items(is_virtual);
