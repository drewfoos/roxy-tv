-- A subtitle track inside a sidecar container (a .mks holding several): its stream
-- number in that file. NULL for a sidecar that is the track itself (.ass, .srt, ...).
ALTER TABLE media_streams ADD COLUMN external_index INTEGER;
