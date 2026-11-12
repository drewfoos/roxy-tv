-- A series request may bring the films that sit between its seasons, but only when the
-- person asking said so (the Request button named them). film_scope remembers that:
-- {"auto":true,"seasons":[AniList ids of the seasons the request covered]}. Empty for
-- every request made before this, so none of them starts fetching films by itself.
ALTER TABLE requests ADD COLUMN film_scope TEXT NOT NULL DEFAULT '';
