-- At most one default profile per login. Concurrent first requests for a new user used
-- to each create a default profile, so some logins have several. Keep the oldest as the
-- default and demote the rest (they may hold watch state, so they are not deleted), give
-- a login that ended up with no default its oldest profile, then let the database refuse
-- a second default from now on.
UPDATE profiles SET is_default = 0
WHERE is_default = 1 AND id <> (
    SELECT p2.id FROM profiles p2
    WHERE p2.user_id = profiles.user_id AND p2.is_default = 1
    ORDER BY p2.created_at ASC, p2.id ASC LIMIT 1
);
UPDATE profiles SET is_default = 1
WHERE id IN (
    SELECT (SELECT p2.id FROM profiles p2 WHERE p2.user_id = u.user_id ORDER BY p2.created_at ASC, p2.id ASC LIMIT 1)
    FROM (SELECT DISTINCT user_id FROM profiles) u
    WHERE NOT EXISTS (SELECT 1 FROM profiles p3 WHERE p3.user_id = u.user_id AND p3.is_default = 1)
);
CREATE UNIQUE INDEX profiles_one_default ON profiles(user_id) WHERE is_default = 1;
