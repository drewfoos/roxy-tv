-- How many detection passes in a row could not fingerprint a source's audio (a file
-- with no sound, or one cut short so its last minutes cannot be read). After a few the
-- detector marks the source as worked on and leaves it alone; before this an episode
-- like that brought its whole season back on every sweep, forever. The row goes when the
-- file is re-probed (replaced), so a new file starts from zero.
ALTER TABLE segment_detection ADD COLUMN failed_passes INTEGER NOT NULL DEFAULT 0;
