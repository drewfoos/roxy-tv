package store

import (
	"context"
	"fmt"
	"strings"
)

// CheckIntegrity looks for anything wrong in the database a scan, a move or a migration
// could leave behind, and describes each problem found (none: an empty list). Used by the
// integration tests after every step and by the backup/restore drills.
//
//   - SQLite's own integrity and foreign-key checks;
//   - a film, episode or extra with no file (scans prune these), or an extra with no
//     title above it;
//   - a file recorded twice, or a source with no file;
//   - two items with one identity (GUID);
//   - watch data for an identity no item has (history no item shows). Moves carry it to
//     the new item, so in a test library this is history a move failed to carry; in a
//     real one it is also the history of files deleted for good, kept in case the same
//     file comes back, so a real library is not expected to pass this one;
//   - a redirect pointing at an item that does not exist.
func (s *Store) CheckIntegrity(ctx context.Context) ([]string, error) {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	var ic string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ic); err != nil {
		return nil, err
	}
	if ic != "ok" {
		add("integrity_check: %s", ic)
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if n > 0 {
		add("foreign_key_check: %d rows point at nothing", n)
	}
	for _, q := range []struct{ what, sql string }{
		{"films, episodes or extras with no file", `SELECT COUNT(*) FROM items WHERE type IN ('movie','episode','extra') AND is_virtual = 0
			AND NOT EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = items.id)`},
		{"extras with no title above them", `SELECT COUNT(*) FROM items WHERE type = 'extra' AND (parent_id IS NULL OR parent_id NOT IN (SELECT id FROM items))`},
		{"files recorded twice", `SELECT COUNT(*) FROM (SELECT path FROM media_parts GROUP BY path HAVING COUNT(*) > 1)`},
		{"sources with no file", `SELECT COUNT(*) FROM media_sources WHERE NOT EXISTS (SELECT 1 FROM media_parts mp WHERE mp.source_id = media_sources.id)`},
		{"identities held by two items", `SELECT COUNT(*) FROM (SELECT guid FROM items GROUP BY guid HAVING COUNT(*) > 1)`},
		{"watch data no item and no redirect reaches (history lost)", `SELECT COUNT(*) FROM user_item_data u
			WHERE (u.position_ms > 0 OR u.played = 1 OR u.is_favorite = 1)
			AND NOT EXISTS (SELECT 1 FROM items i WHERE i.guid = u.item_guid)`},
		{"redirects to items that do not exist", `SELECT COUNT(*) FROM item_redirects r WHERE NOT EXISTS (SELECT 1 FROM items i WHERE i.id = r.new_id)
			AND NOT EXISTS (SELECT 1 FROM item_redirects r2 WHERE r2.old_id = r.new_id)`},
	} {
		var c int
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", q.what, err)
		}
		if c > 0 {
			add("%d %s", c, q.what)
		}
	}
	return problems, nil
}

// LostWatchData lists the identities whose watch data no item holds (see CheckIntegrity),
// for a test or drill to say which.
func (s *Store) LostWatchData(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT item_guid FROM user_item_data u
		WHERE (u.position_ms > 0 OR u.played = 1 OR u.is_favorite = 1)
		AND NOT EXISTS (SELECT 1 FROM items i WHERE i.guid = u.item_guid) LIMIT 20`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return "", err
		}
		out = append(out, g)
	}
	return strings.Join(out, ", "), rows.Err()
}
