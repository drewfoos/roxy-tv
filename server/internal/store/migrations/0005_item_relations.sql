-- Provider-linked titles that are not seasons of the item: movies, OVAs, side stories.
-- Resolved against the library at read time through item_provider_ids.
CREATE TABLE item_relations (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    relation    TEXT NOT NULL,
    title       TEXT NOT NULL,
    format      TEXT,
    year        INTEGER,
    poster_url  TEXT,
    episodes    INTEGER,
    PRIMARY KEY (item_id, provider, provider_id)
);
CREATE INDEX item_relations_target ON item_relations(provider, provider_id);
