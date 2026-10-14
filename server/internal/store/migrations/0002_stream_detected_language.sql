-- Language sniffed from the subtitle text at ingest (ISO 639-2/B), distinct from the
-- container's language tag, which release groups get wrong often enough to matter.
ALTER TABLE media_streams ADD COLUMN detected_language TEXT;
