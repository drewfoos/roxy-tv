-- The library folder as a directory (device, inode, creation time), recorded by the last
-- scan that found files in it. A later scan that finds it empty can tell a folder whose
-- files were really removed (the same directory) from an unmounted drive or the empty
-- folder Docker makes for a missing bind mount (another directory). '' = not known yet.
ALTER TABLE libraries ADD COLUMN root_identity TEXT NOT NULL DEFAULT '';
