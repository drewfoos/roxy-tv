-- Placeholders kept the library id they were first created with, so a series moved to
-- another library (or created under the wrong one) left its virtual seasons and
-- episodes behind. Every season and episode takes its parent's library: seasons first,
-- then episodes from their (now repaired) seasons.
UPDATE items SET library_id = (SELECT p.library_id FROM items p WHERE p.id = items.parent_id)
    WHERE type = 'season' AND parent_id IS NOT NULL
    AND library_id != (SELECT p.library_id FROM items p WHERE p.id = items.parent_id);
UPDATE items SET library_id = (SELECT p.library_id FROM items p WHERE p.id = items.parent_id)
    WHERE type = 'episode' AND parent_id IS NOT NULL
    AND library_id != (SELECT p.library_id FROM items p WHERE p.id = items.parent_id);
