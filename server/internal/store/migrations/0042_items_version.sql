-- A counter of every change to items (insert, update, delete), so a list read in pages can
-- tell it changed between two pages even when its total did not (one episode removed and
-- another added by a scan): the clients start the read over when it moves.
CREATE TABLE items_version (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    n  INTEGER NOT NULL
);
INSERT INTO items_version (id, n) VALUES (1, 0);
CREATE TRIGGER items_version_insert AFTER INSERT ON items BEGIN UPDATE items_version SET n = n + 1 WHERE id = 1; END;
CREATE TRIGGER items_version_update AFTER UPDATE ON items BEGIN UPDATE items_version SET n = n + 1 WHERE id = 1; END;
CREATE TRIGGER items_version_delete AFTER DELETE ON items BEGIN UPDATE items_version SET n = n + 1 WHERE id = 1; END;
