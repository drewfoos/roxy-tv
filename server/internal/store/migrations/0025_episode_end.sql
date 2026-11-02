-- The last episode a multi-episode file holds ("Show - S01E01-E03 - ..." covers E1..E3),
-- NULL for a single episode. Placeholders for the episodes such a file covers are not
-- created (and the shadow cleanup drops existing ones). Filled by the scanner, and for
-- files scanned before this column existed by the next metadata pass of their series.
ALTER TABLE items ADD COLUMN index_aired_end INTEGER;
