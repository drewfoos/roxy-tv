-- The next stream number a new sidecar subtitle of this source may take: above every
-- number a sidecar of it has ever had. A profile's saved subtitle choice is a stream
-- number, so a number once given to a sidecar is never handed to a different file, even
-- after that sidecar is removed and another added in a later scan. 0 = none given yet
-- (existing rows: the numbers in use are the floor until the next ingest raises it).
ALTER TABLE media_sources ADD COLUMN sidecar_next_index INTEGER NOT NULL DEFAULT 0;
