-- Who asked, as shown to the household: the profile's name when the request came from
-- a profile (several people share one login), else the user's name.
ALTER TABLE requests ADD COLUMN requested_by TEXT;
