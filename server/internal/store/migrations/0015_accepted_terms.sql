-- Which version of the Terms of Use each login accepted (NULL = never). Per user, not per
-- profile: the household shares the login.
ALTER TABLE users ADD COLUMN accepted_terms TEXT;
