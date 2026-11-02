package store

import (
	"context"
	"time"
)

// SeriesCharacter is one character saved for a series (the avatar suggestions).
type SeriesCharacter struct {
	Source string `json:"-"`
	ID     int    `json:"id"` // AniList character id, or minus the TMDB person id
	Name   string `json:"name"`
	Image  string `json:"image"`
	Series string `json:"series"`
}

// SeriesNeedingCharacters lists series (newest first) that have a provider id to look
// characters up by and have not been looked up, or failed before retryBefore with fewer
// than three tries. limit keeps each pass small however many shows arrive at once.
func (s *Store) SeriesNeedingCharacters(ctx context.Context, limit int, retryBefore time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id FROM items i
		LEFT JOIN series_characters_state st ON st.series_id = i.id
		WHERE i.type = 'series'
		  AND (st.series_id IS NULL OR (st.ok = 0 AND st.attempts < 3 AND st.fetched_at < ?))
		  AND EXISTS (SELECT 1 FROM item_provider_ids p WHERE p.item_id = i.id AND p.provider IN ('anilist', 'tmdb'))
		ORDER BY i.date_created DESC LIMIT ?`, retryBefore.UTC().Format(TimeFormat), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SaveSeriesCharacters records a lookup: on success the series' characters are replaced
// with chars (possibly none); on failure only the attempt is counted.
func (s *Store) SaveSeriesCharacters(ctx context.Context, seriesID string, chars []SeriesCharacter, ok bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if ok {
		if _, err := tx.ExecContext(ctx, `DELETE FROM series_characters WHERE series_id = ?`, seriesID); err != nil {
			return err
		}
		for i, c := range chars {
			if _, err := tx.ExecContext(ctx, `INSERT INTO series_characters (series_id, pos, source, char_id, name, image, series_title) VALUES (?,?,?,?,?,?,?)`,
				seriesID, i, c.Source, c.ID, c.Name, c.Image, c.Series); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO series_characters_state (series_id, fetched_at, ok, attempts) VALUES (?, ?, ?, 1)
		ON CONFLICT(series_id) DO UPDATE SET fetched_at = excluded.fetched_at, ok = excluded.ok, attempts = series_characters_state.attempts + 1`,
		seriesID, Now(), boolInt(ok)); err != nil {
		return err
	}
	return tx.Commit()
}

// SuggestedCharacters is "From your library": up to perSeries characters of each series,
// newest series first, at most max in all, each character once.
func (s *Store) SuggestedCharacters(ctx context.Context, perSeries, max int) ([]SeriesCharacter, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.source, c.char_id, c.name, c.image, c.series_title FROM series_characters c
		JOIN items i ON i.id = c.series_id
		WHERE c.pos < ?
		ORDER BY i.date_created DESC, c.series_id, c.pos LIMIT ?`, perSeries, max*2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeriesCharacter{}
	seen := map[int]bool{}
	for rows.Next() {
		var c SeriesCharacter
		if err := rows.Scan(&c.Source, &c.ID, &c.Name, &c.Image, &c.Series); err != nil {
			return nil, err
		}
		if seen[c.ID] || len(out) >= max {
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out, rows.Err()
}
