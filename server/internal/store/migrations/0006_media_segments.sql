-- Skippable ranges of a media source (intro, outro, recap, preview). `origin` says how
-- they were found: "chapters" (heuristic over the file's chapter markers), later
-- "fingerprint" (audio match across a season) or "manual".
CREATE TABLE media_segments (
    source_id TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
    type      TEXT NOT NULL CHECK (type IN ('intro', 'outro', 'recap', 'preview')),
    start_ms  INTEGER NOT NULL,
    end_ms    INTEGER NOT NULL,
    origin    TEXT NOT NULL,
    PRIMARY KEY (source_id, type)
);
