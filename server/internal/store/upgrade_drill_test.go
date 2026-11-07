package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpgradeFromOlderVersions is the restore drill CI runs: a database as older versions
// of Roxy left it (built by applying the migrations only up to that version, then filled
// with synthetic data of every kind: libraries, a series with episodes, a film with two
// versions, a sidecar subtitle, a profile's watch history, provider ids, artwork) is
// opened by this build, as restoring that version's backup would. Nothing may be lost,
// CheckIntegrity must find nothing, the watch history must read back, and the migrations
// in between must have done their work. The operational drill on a real backup copy is
// TestRestoreDrill (ROXY_RESTORE_COPY); no real database is kept in the repository.
func TestUpgradeFromOlderVersions(t *testing.T) {
	for _, from := range []string{
		"0028_one_default_profile.sql", // before the departures, extras and sidecar work
		"0032_special_placement.sql",   // before sidecar language codes and extras
		"0036_version_labels.sql",      // before .mks tracks
	} {
		t.Run(strings.SplitN(from, "_", 2)[0], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "roxy.db")
			old, err := openUpTo(path, from)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`INSERT INTO libraries (id, name, kind, root, created_at) VALUES (1, 'Anime', 'series', '/m/Anime', 'x'), (2, 'Movies', 'movies', '/m/Movies', 'x')`,
				`INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('s1', 'tvdb:series:1', 1, 'series', 'Show', 'x', 'x')`,
				`INSERT INTO items (id, guid, library_id, type, parent_id, title, index_aired, date_created, date_modified) VALUES ('se1', 'tvdb:series:1:season:1', 1, 'season', 's1', 'Season 1', 1, 'x', 'x')`,
				`INSERT INTO items (id, guid, library_id, type, parent_id, title, index_aired, season_aired, date_created, date_modified) VALUES
					('e1', 'tvdb:series:1:s01e0001', 1, 'episode', 'se1', 'One', 1, 1, 'x', 'x'),
					('e2', 'tvdb:series:1:s01e0002', 1, 'episode', 'se1', 'Two', 2, 1, 'x', 'x')`,
				`INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('m1', 'path:movie:abc', 2, 'movie', 'Film', 'x', 'x')`,
				`INSERT INTO media_sources (id, item_id, name, is_primary) VALUES ('src-e1', 'e1', 'Show - S01E01.mkv', 1), ('src-e2', 'e2', 'Show - S01E02.mkv', 1),
					('src-m1a', 'm1', 'Film - 1080p.mkv', 1), ('src-m1b', 'm1', 'Film - 2160p.mkv', 1)`,
				`INSERT INTO media_parts (id, source_id, part_index, path, size, mtime) VALUES
					('p-e1', 'src-e1', 0, '/m/Anime/Show/Show - S01E01.mkv', 100, 'x'), ('p-e2', 'src-e2', 0, '/m/Anime/Show/Show - S01E02.mkv', 200, 'x'),
					('p-m1a', 'src-m1a', 0, '/m/Movies/Film/Film - 1080p.mkv', 300, 'x'), ('p-m1b', 'src-m1b', 0, '/m/Movies/Film/Film - 2160p.mkv', 400, 'x')`,
				`INSERT INTO media_streams (source_id, stream_index, type, codec, width, height) VALUES
					('src-e1', 0, 'video', 'h264', 1920, 1080), ('src-e2', 0, 'video', 'h264', 1920, 1080),
					('src-m1a', 0, 'video', 'hevc', 1920, 802), ('src-m1b', 0, 'video', 'hevc', 3840, 1604)`,
				`INSERT INTO media_streams (source_id, stream_index, type, codec, language, is_external, external_path, is_text_format) VALUES
					('src-e1', 5, 'subtitle', 'subrip', 'en', 1, '/m/Anime/Show/Show - S01E01.en.srt', 1)`,
				`INSERT INTO users (id, name, created_at) VALUES ('u1', 'drew', 'x')`,
				`INSERT INTO profiles (id, user_id, name, created_at) VALUES ('p1', 'u1', 'drew', 'x')`,
				`INSERT INTO user_item_data (scope_id, item_guid, position_ms, last_played_at) VALUES ('p1', 'tvdb:series:1:s01e0002', 1500, '2026-11-01T00:00:00.000Z')`,
				`INSERT INTO user_item_data (scope_id, item_guid, played, play_count) VALUES ('p1', 'path:movie:abc', 1, 2)`,
				`INSERT INTO user_item_data (scope_id, item_guid, is_favorite) VALUES ('p1', 'tvdb:series:1', 1)`,
				`INSERT INTO item_provider_ids (item_id, provider, value) VALUES ('s1', 'tmdb', '1')`,
				`INSERT INTO item_images (item_id, type, path, fetched_at) VALUES ('s1', 'poster', '/c/s1/poster.jpg', 'x')`,
			} {
				if _, err := old.db.ExecContext(ctx, q); err != nil {
					t.Fatalf("seeding the %s database: %v\n%s", from, err, q)
				}
			}
			tables := []string{"libraries", "items", "media_sources", "media_parts", "media_streams", "user_item_data", "users", "profiles", "item_provider_ids", "item_images"}
			before := map[string]int{}
			for _, tb := range tables {
				var n int
				_ = old.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tb).Scan(&n)
				before[tb] = n
			}
			old.Close()

			s, err := Open(path) // the restore: this build opens the old database
			if err != nil {
				t.Fatalf("upgrading from %s: %v", from, err)
			}
			defer s.Close()
			for _, tb := range tables {
				var n int
				if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tb).Scan(&n); err != nil || n != before[tb] {
					t.Errorf("%s: %d rows, had %d (%v)", tb, n, before[tb], err)
				}
			}
			if probs, err := s.CheckIntegrity(ctx); err != nil || len(probs) > 0 {
				t.Fatalf("after the upgrade: %v %v", probs, err)
			}
			// Watch history reads back as it was.
			if d, _ := s.GetUserItemData(ctx, "p1", "tvdb:series:1:s01e0002"); d == nil || d.PositionMs != 1500 {
				t.Errorf("episode position: %+v", d)
			}
			if d, _ := s.GetUserItemData(ctx, "p1", "path:movie:abc"); d == nil || !d.Played || d.PlayCount != 2 {
				t.Errorf("film watched: %+v", d)
			}
			if d, _ := s.GetUserItemData(ctx, "p1", "tvdb:series:1"); d == nil || !d.IsFavorite {
				t.Errorf("watchlist: %+v", d)
			}
			// What the migrations since then do.
			srcs, err := s.SourcesForItem(ctx, "e1")
			if err != nil || len(srcs) != 1 {
				t.Fatal(srcs, err)
			}
			for _, st := range srcs[0].Streams {
				if st.IsExternal && st.Language != "eng" && from < "0033" {
					t.Errorf("sidecar language %q, want eng (0033)", st.Language)
				}
			}
			if v, _ := s.SourcesForItem(ctx, "m1"); from < "0035" && (len(v) != 2 || v[0].ID != "src-m1b" || !v[0].IsPrimary || v[1].IsPrimary || v[0].VersionLabel != "2160p" || v[1].VersionLabel != "1080p") {
				t.Errorf("versions after 0035/0036: %+v", v)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, parent_id, title, date_created, date_modified) VALUES ('x1', 'extra:1', 2, 'extra', 'm1', 'Trailer', 'x', 'x')`); err != nil {
				t.Errorf("an extra after 0034: %v", err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, title, date_created, date_modified) VALUES ('b1', 'bogus:1', 2, 'bogus', 't', 'x', 'x')`); err == nil {
				t.Error("the items type check no longer refuses unknown types")
			}
			for _, tb := range []string{"departures", "item_redirects"} {
				if _, err := s.db.ExecContext(ctx, `SELECT COUNT(*) FROM `+tb); err != nil {
					t.Errorf("%s: %v", tb, err)
				}
			}
		})
	}
}
