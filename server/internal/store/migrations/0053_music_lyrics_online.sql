-- Lyrics found online for a song that has none of its own (see music/lrclib.go), and when
-- the song was last asked about (Unix seconds; 0 = never). A song with a lyrics file or
-- a lyrics tag of its own keeps using that: these are only for the ones without.
ALTER TABLE music_tracks ADD COLUMN lyrics_online TEXT NOT NULL DEFAULT '';
ALTER TABLE music_tracks ADD COLUMN lyrics_online_at INTEGER NOT NULL DEFAULT 0;

-- A lyrics tag that holds an address and no lyrics (what a release group writes there in
-- place of them) is not the song's lyrics. New files are checked as they are read; the
-- ones already in are cleared here.
UPDATE music_tracks SET lyrics_text = ''
 WHERE length(trim(lyrics_text)) < 80
   AND (lower(lyrics_text) LIKE '%www.%' OR lower(lyrics_text) LIKE '%t.me/%' OR lower(lyrics_text) LIKE '%http://%' OR lower(lyrics_text) LIKE '%https://%');
UPDATE music_tracks SET lyrics = '' WHERE lyrics = 'embedded' AND lyrics_text = '';
