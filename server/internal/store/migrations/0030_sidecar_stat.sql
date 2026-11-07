-- The size and modification time of a sidecar subtitle file when it was ingested, so a
-- scan notices a sidecar replaced under the same name (its detected language is read from
-- its text at ingest). NULL for rows from before this: the next scan records them without
-- re-ingesting anything.
ALTER TABLE media_streams ADD COLUMN external_size INTEGER;
ALTER TABLE media_streams ADD COLUMN external_mtime TEXT;
