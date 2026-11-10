-- items_fts was filled with every new item's title and never read: search uses LIKE on
-- items, and nothing updated a row when a title changed or removed it with its item, so
-- it only held stale titles. The server no longer writes to it; this empties it. The
-- table itself stays, so an older build put back on this database can still add items.
INSERT INTO items_fts (items_fts) VALUES ('delete-all');
