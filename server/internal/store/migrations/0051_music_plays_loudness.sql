-- Every counted listen. `reported` is the time the player gave for it (Unix ms), as it
-- gave it: the same report sent twice (a retry after a lost answer) is the same row, so a
-- listen is counted once whatever the network does. `played_at` is when it was listened
-- to, as kept: the player's time where that can be believed (not in the future, not
-- months old), else the time the report arrived.
CREATE TABLE music_plays (
    scope_id  TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    track_id  TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    reported  INTEGER NOT NULL,
    played_at TEXT NOT NULL,
    PRIMARY KEY (scope_id, track_id, reported)
);
CREATE INDEX music_plays_recent ON music_plays(scope_id, played_at DESC);

-- How loud a song is, measured by the server from the audio itself (EBU R 128): the
-- integrated loudness in LUFS and the true peak in dBTP. loudness_mtime is the file time
-- the measurement belongs to (a changed file is measured again); loudness_tries counts
-- the measurements that failed, so a file that cannot be decoded is given up on.
ALTER TABLE music_tracks ADD COLUMN loudness REAL;
ALTER TABLE music_tracks ADD COLUMN true_peak REAL;
ALTER TABLE music_tracks ADD COLUMN loudness_mtime INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_tracks ADD COLUMN loudness_tries INTEGER NOT NULL DEFAULT 0;
-- An album's loudness: that of its songs played one after another.
ALTER TABLE music_albums ADD COLUMN loudness REAL;
