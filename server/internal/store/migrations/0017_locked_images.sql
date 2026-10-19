-- An image the admin chose by hand stays put: metadata refreshes skip locked ones.
ALTER TABLE item_images ADD COLUMN locked INTEGER NOT NULL DEFAULT 0;
