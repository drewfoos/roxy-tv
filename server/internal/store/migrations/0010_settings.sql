-- Server settings edited from the app (admin), as JSON documents by key. Environment
-- variables remain the defaults for anything not saved here.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
