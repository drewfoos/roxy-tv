-- How many people a title's community rating comes from (TMDB's vote count, the number of
-- scores on AniList), shown beside the rating: "4.9 * (42.9K)". Empty until a title's
-- next metadata refresh.
ALTER TABLE items ADD COLUMN rating_count INTEGER;
