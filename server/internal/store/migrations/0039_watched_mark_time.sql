-- When the watched state was last set by hand (marked watched or unwatched). Offline
-- reports apply only if nothing newer happened to the item; unmarking used to leave no
-- time behind, so an older offline "watched" undid a newer "unwatched". NULL: never.
ALTER TABLE user_item_data ADD COLUMN marked_at TEXT;
