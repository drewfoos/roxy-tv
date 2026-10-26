package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// TitleStatus is one series or movie as the Library settings screen lists it.
type TitleStatus struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Year        *int              `json:"year,omitempty"`
	Matched     bool              `json:"matched"` // carries a TMDB or AniList id
	Locked      bool              `json:"locked"`  // details edited by hand; refreshes skip it
	ProviderIDs map[string]string `json:"providerIds,omitempty"`
}

// LibraryTitles lists every series and movie that has files, unmatched ones first.
func (s *Store) LibraryTitles(ctx context.Context) ([]TitleStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, title, year, metadata_locked FROM items
		WHERE type IN ('series', 'movie') AND is_virtual = 0 ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var out []TitleStatus
	idx := map[string]int{}
	for rows.Next() {
		var t TitleStatus
		var year sql.NullInt64
		var locked int
		if err := rows.Scan(&t.ID, &t.Type, &t.Title, &year, &locked); err != nil {
			rows.Close()
			return nil, err
		}
		if year.Valid {
			y := int(year.Int64)
			t.Year = &y
		}
		t.Locked = locked == 1
		t.ProviderIDs = map[string]string{}
		idx[t.ID] = len(out)
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pr, err := s.db.QueryContext(ctx, `SELECT p.item_id, p.provider, p.value FROM item_provider_ids p
		JOIN items i ON i.id = p.item_id WHERE i.type IN ('series', 'movie')`)
	if err != nil {
		return nil, err
	}
	defer pr.Close()
	for pr.Next() {
		var id, p, v string
		if err := pr.Scan(&id, &p, &v); err != nil {
			return nil, err
		}
		if i, ok := idx[id]; ok {
			out[i].ProviderIDs[p] = v
		}
	}
	for i := range out {
		ids := out[i].ProviderIDs
		out[i].Matched = ids["tmdb"] != "" || ids["anilist"] != ""
	}
	// Unmatched first: those are the ones that need a hand.
	sorted := make([]TitleStatus, 0, len(out))
	for _, t := range out {
		if !t.Matched && !t.Locked {
			sorted = append(sorted, t)
		}
	}
	for _, t := range out {
		if t.Matched || t.Locked {
			sorted = append(sorted, t)
		}
	}
	return sorted, pr.Err()
}

// MetadataLocked reports whether an item's details were set by hand.
func (s *Store) MetadataLocked(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT metadata_locked FROM items WHERE id = ?`, id).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return n == 1, err
}

// SetMetadataLocked locks or unlocks an item's details.
func (s *Store) SetMetadataLocked(ctx context.Context, id string, locked bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET metadata_locked = ? WHERE id = ?`, boolInt(locked), id)
	return err
}

// ReplaceProviderIDs drops every provider id of an item and sets ids instead (Identify:
// a wrong match's ids must not steer the next lookup).
func (s *Store) ReplaceProviderIDs(ctx context.Context, id string, ids map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_provider_ids WHERE item_id = ?`, id); err != nil {
		return err
	}
	for p, v := range ids {
		if v == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO item_provider_ids (item_id, provider, value) VALUES (?,?,?)`, id, p, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ManualMetadata is what the admin types on the Edit details screen. Every field is
// written as given (an empty overview clears it), and the item is locked.
type ManualMetadata struct {
	Title     string   `json:"title"`
	Year      *int     `json:"year"`
	Overview  string   `json:"overview"`
	Genres    []string `json:"genres"`
	Studio    string   `json:"studio"`
	Premiered string   `json:"premiered"` // YYYY-MM-DD
	Locked    bool     `json:"locked"`
}

// EditItemMetadata saves hand-entered details.
func (s *Store) EditItemMetadata(ctx context.Context, id string, m ManualMetadata) error {
	var genres sql.NullString
	clean := make([]string, 0, len(m.Genres))
	for _, g := range m.Genres {
		if g = strings.TrimSpace(g); g != "" {
			clean = append(clean, g)
		}
	}
	if len(clean) > 0 {
		b, _ := json.Marshal(clean)
		genres = nullStr(string(b))
	}
	now := Now()
	_, err := s.db.ExecContext(ctx, `UPDATE items SET title = ?, year = ?, overview = ?, genres = ?, studio = ?, premiere_date = ?,
		metadata_locked = ?, metadata_refreshed_at = COALESCE(metadata_refreshed_at, ?), date_modified = ? WHERE id = ?`,
		strings.TrimSpace(m.Title), intPtrNull(m.Year), nullStr(strings.TrimSpace(m.Overview)), genres, nullStr(strings.TrimSpace(m.Studio)),
		nullStr(strings.TrimSpace(m.Premiered)), boolInt(m.Locked), now, now, id)
	return err
}
