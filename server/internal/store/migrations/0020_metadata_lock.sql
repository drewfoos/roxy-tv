-- A title the admin edited by hand (or a custom show no provider knows, like One Pace)
-- keeps its details: metadata refreshes skip locked items until Identify unlocks them.
ALTER TABLE items ADD COLUMN metadata_locked INTEGER NOT NULL DEFAULT 0;
