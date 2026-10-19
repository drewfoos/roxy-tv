-- What Sonarr / Radarr told us and what Roxy did about it, for the admin's Webhooks page.
CREATE TABLE webhook_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    at         TEXT NOT NULL,
    source     TEXT NOT NULL,          -- sonarr | radarr
    event_type TEXT NOT NULL,          -- Download, Rename, EpisodeFileDelete, ...
    title      TEXT NOT NULL DEFAULT '',
    path       TEXT NOT NULL DEFAULT '',
    result     TEXT NOT NULL DEFAULT '' -- ingested | removed | renamed | ignored | error: ...
);
CREATE INDEX webhook_events_at ON webhook_events(at);
