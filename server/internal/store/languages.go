package store

import (
	"context"
	"strings"
)

// AniListTagged reports which of the given items are anime: they carry an AniList provider
// id (matched as anime) or sit in a library in anime mode (even when AniList has not
// answered for them yet).
func (s *Store) AniListTagged(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT item_id FROM item_provider_ids WHERE provider = 'anilist' AND item_id IN (`+ph+`)
		UNION SELECT i.id FROM items i JOIN libraries l ON l.id = i.library_id WHERE l.anime_mode = 1 AND i.id IN (`+ph+`)`, append(args, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AudioLanguagesFor returns, per top-level item (series or movie), the distinct audio
// languages across its media in one query: a movie's own sources, or every episode under
// a series' seasons. Items without tagged audio are absent from the map.
func (s *Store) AudioLanguagesFor(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT top.id, lower(ms.language)
		FROM items top
		JOIN items e ON e.id = top.id OR e.parent_id = top.id OR e.parent_id IN (SELECT id FROM items WHERE parent_id = top.id)
		JOIN media_sources src ON src.item_id = e.id
		JOIN media_streams ms ON ms.source_id = src.id AND ms.type = 'audio' AND ms.language IS NOT NULL AND ms.language <> ''
		WHERE top.id IN (`+ph+`)
		GROUP BY top.id, lower(ms.language)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, lang string
		if err := rows.Scan(&id, &lang); err != nil {
			return nil, err
		}
		out[id] = append(out[id], lang)
	}
	return out, rows.Err()
}
