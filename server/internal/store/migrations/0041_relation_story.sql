-- Whether a related title is part of the main story: a film AniList links as a prequel
-- or sequel of the series, or one on the way between two of its seasons (Bunny Girl
-- Senpai's three films between Season 1 and Season 2). Side stories, recaps and
-- spin-offs are not. A requested series brings its story films along.
ALTER TABLE item_relations ADD COLUMN story INTEGER NOT NULL DEFAULT 0;
