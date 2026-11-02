-- Which sources the segment detector already worked on, so a restart does not redo a
-- season just because an episode legitimately has no intro or outro (finales, specials,
-- shows without an OP). `version` is the detector's version: raising it re-runs every
-- season once. `aniskip_mal` is the MyAnimeList id AniSkip was asked with ('' = it had
-- none), so a season that later gets an id asks once. Re-probing a source (its file
-- changed) deletes its row.
CREATE TABLE segment_detection (
    source_id   TEXT PRIMARY KEY REFERENCES media_sources(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    detected_at TEXT NOT NULL,
    aniskip_mal TEXT
);
-- Sources with both fingerprints cached went through a detection pass already (version
-- 1, the first versioned detector): no full re-run after the upgrade. AniSkip was never
-- recorded, so seasons with a MAL id ask it once more.
INSERT INTO segment_detection (source_id, version, detected_at)
    SELECT source_id, 1, MAX(created_at) FROM audio_fingerprints GROUP BY source_id HAVING COUNT(*) = 2;
