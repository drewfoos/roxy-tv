-- Two attachments of one file may carry one name. They were extracted to one cache file,
-- the second over the first, and both rows pointed at it: where they are different
-- fonts, the first was lost. Extraction now keeps each under a name of its own; forgetting
-- where these files' fonts were extracted makes the next request for one extract them
-- all again (the cache files stay until then and are written over).
UPDATE attachments SET extracted_path = NULL, extracted_at = NULL
WHERE source_id IN (
    SELECT source_id FROM attachments GROUP BY source_id, filename HAVING COUNT(*) > 1
);
