-- media_sources.probe_json held ffprobe's whole output for every file. It was written at
-- each probe and never read: everything Roxy uses from a probe is in media_streams,
-- media_parts and attachments. On a library of a few thousand files it had grown to a
-- third of the database. The values are removed and no longer written; the column itself
-- stays, so an older build (which still writes it) keeps working on this database.
--
-- roxy:vacuum
UPDATE media_sources SET probe_json = NULL WHERE probe_json IS NOT NULL;
