-- Content advisories ("Sexual Content", "Violence", ...) as a JSON array, from TMDB's
-- rating descriptors where present and AniList's tags otherwise.
ALTER TABLE items ADD COLUMN advisories TEXT;
