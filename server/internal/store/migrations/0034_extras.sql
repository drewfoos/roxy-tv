-- Extras (trailers, featurettes, behind the scenes, deleted scenes, interviews, scenes,
-- shorts, clips, theme videos, other): items of type 'extra' under the film or show they
-- belong to, with their kind. The type's CHECK constraint is widened in place, the way
-- SQLite documents for a constraint every existing row already satisfies: rebuilding
-- items would cascade-delete every table that references it. The column is added first:
-- ADD COLUMN splices into the table's SQL at an offset taken from the parsed schema, so
-- it must not run after the text changed. The runner bumps schema_version afterwards
-- (store.migrate), which makes every connection re-read the schema.
ALTER TABLE items ADD COLUMN extra_type TEXT;
PRAGMA writable_schema = ON;
UPDATE sqlite_master SET sql = replace(sql,
    'CHECK (type IN (''movie'', ''series'', ''season'', ''episode''))',
    'CHECK (type IN (''movie'', ''series'', ''season'', ''episode'', ''extra''))')
WHERE type = 'table' AND name = 'items';
PRAGMA writable_schema = OFF;
