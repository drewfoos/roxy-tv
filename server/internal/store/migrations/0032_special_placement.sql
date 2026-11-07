-- Where a special belongs in story order (from its .nfo: airsbefore_season /
-- airsbefore_episode / airsafter_season, or Kodi's displayseason / displayepisode): a
-- film between two seasons, an OVA before an episode. Next-up follows it.
ALTER TABLE items ADD COLUMN airs_before_season INTEGER;
ALTER TABLE items ADD COLUMN airs_before_episode INTEGER;
ALTER TABLE items ADD COLUMN airs_after_season INTEGER;
