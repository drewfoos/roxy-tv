-- How loud a song's measuring blocks are (400 ms each, counted by tenths of a LU): what
-- an album's loudness is measured from. An album's loudness is not the mean of its
-- songs': the standard leaves out the blocks far under the level of the whole, and which
-- those are depends on the whole album. A song measured before this has none and is
-- measured once more (it keeps the loudness it has until then).
ALTER TABLE music_tracks ADD COLUMN loudness_blocks BLOB;
-- An album's true peak: the highest of its songs'. Songs of an album played together are
-- turned up or down by the same amount, so the room there is to turn them up is the
-- album's, not each song's own.
ALTER TABLE music_albums ADD COLUMN true_peak REAL;
-- Album figures worked out the old way (a mean of the songs' loudness) are dropped; they
-- return as the songs are measured again.
UPDATE music_albums SET loudness = NULL WHERE loudness IS NOT NULL;
