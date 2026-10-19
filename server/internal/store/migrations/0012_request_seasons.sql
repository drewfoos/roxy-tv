-- Season-level requests (request app): NULL / '' = the whole series, else a JSON array of
-- season numbers such as "[2,3]". Movies never set it.
ALTER TABLE requests ADD COLUMN seasons TEXT;
