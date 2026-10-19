-- Chromaprint audio fingerprints of the head (first minutes) and tail (last minutes) of a
-- media source, for intro/outro detection by matching repeated audio across a season.
-- `points` is the raw fingerprint: little-endian uint32 per ~0.124 s of audio.
CREATE TABLE audio_fingerprints (
    source_id  TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('head', 'tail')),
    start_ms   INTEGER NOT NULL,
    length_ms  INTEGER NOT NULL,
    points     BLOB NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (source_id, kind)
);
