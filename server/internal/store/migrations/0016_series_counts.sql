-- Series facts a title page needs before its own fetches land: how many seasons the
-- provider knows (not how many the library holds) and how many related titles (movies,
-- OVAs, side stories) link to it. Both come with every list item.
ALTER TABLE items ADD COLUMN season_count INTEGER;
ALTER TABLE items ADD COLUMN related_count INTEGER NOT NULL DEFAULT 0;
