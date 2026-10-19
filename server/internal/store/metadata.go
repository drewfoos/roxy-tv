package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ItemMetadata is what a provider lookup contributes to an item. Empty fields are left
// untouched in the database, so a thin result never erases a richer earlier one.
type ItemMetadata struct {
	Title           string
	Year            *int
	Overview        string
	Tagline         string
	Genres          []string
	CommunityRating float64 // 0 = unknown
	OfficialRating  string
	Premiered       string // YYYY-MM-DD
	Status          string
	Studio          string
	SeasonCount     int // series: 0 = unknown
	ProviderIDs     map[string]string
}

// UpdateItemMetadata applies provider metadata and stamps metadata_refreshed_at.
func (s *Store) UpdateItemMetadata(ctx context.Context, id string, m ItemMetadata) error {
	var genres sql.NullString
	if len(m.Genres) > 0 {
		b, _ := json.Marshal(m.Genres)
		genres = nullStr(string(b))
	}
	now := Now()
	_, err := s.db.ExecContext(ctx, `UPDATE items SET
		title = COALESCE(?, title), year = COALESCE(?, year), overview = COALESCE(?, overview), tagline = COALESCE(?, tagline),
		genres = COALESCE(?, genres), community_rating = COALESCE(?, community_rating), official_rating = COALESCE(?, official_rating),
		premiere_date = COALESCE(?, premiere_date), status = COALESCE(?, status), studio = COALESCE(?, studio),
		season_count = COALESCE(?, season_count), metadata_refreshed_at = ?, date_modified = ? WHERE id = ?`,
		nullStr(m.Title), intPtrNull(m.Year), nullStr(m.Overview), nullStr(m.Tagline), genres, nullFloat(m.CommunityRating),
		nullStr(m.OfficialRating), nullStr(m.Premiered), nullStr(m.Status), nullStr(m.Studio), nullInt(m.SeasonCount), now, now, id)
	if err != nil {
		return err
	}
	for p, v := range m.ProviderIDs {
		if v == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO item_provider_ids (item_id, provider, value) VALUES (?,?,?)
			ON CONFLICT(item_id, provider) DO UPDATE SET value = excluded.value`, id, p, v); err != nil {
			return err
		}
	}
	return nil
}

// ItemImage is one cached image file for an item.
type ItemImage struct {
	ItemID    string
	Type      string
	Path      string
	Width     int
	Height    int
	SourceURL string
	FetchedAt time.Time
}

func (s *Store) SetItemImage(ctx context.Context, img ItemImage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO item_images (item_id, type, path, width, height, source_url, fetched_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(item_id, type) DO UPDATE SET path = excluded.path, width = excluded.width, height = excluded.height,
		source_url = excluded.source_url, fetched_at = excluded.fetched_at`,
		img.ItemID, img.Type, img.Path, nullInt(img.Width), nullInt(img.Height), nullStr(img.SourceURL), Now())
	return err
}

func (s *Store) GetItemImage(ctx context.Context, itemID, imageType string) (*ItemImage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT item_id, type, path, width, height, source_url, fetched_at FROM item_images WHERE item_id = ? AND type = ?`, itemID, imageType)
	img, err := scanImage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return img, err
}

func (s *Store) ImagesForItem(ctx context.Context, itemID string) ([]ItemImage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, type, path, width, height, source_url, fetched_at FROM item_images WHERE item_id = ? ORDER BY type`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemImage
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *img)
	}
	return out, rows.Err()
}

func scanImage(sc interface{ Scan(...any) error }) (*ItemImage, error) {
	var img ItemImage
	var w, h sql.NullInt64
	var src sql.NullString
	var at string
	if err := sc.Scan(&img.ItemID, &img.Type, &img.Path, &w, &h, &src, &at); err != nil {
		return nil, err
	}
	img.Width, img.Height, img.SourceURL = int(w.Int64), int(h.Int64), src.String
	img.FetchedAt = ParseTime(at)
	return &img, nil
}

// TopLevelID walks parents until it reaches a series or movie and returns its id.
func (s *Store) TopLevelID(ctx context.Context, itemID string) (string, error) {
	id := itemID
	for depth := 0; depth < 4; depth++ {
		var parent sql.NullString
		var typ string
		err := s.db.QueryRowContext(ctx, `SELECT type, parent_id FROM items WHERE id = ?`, id).Scan(&typ, &parent)
		if err != nil {
			return "", err
		}
		if !parent.Valid || typ == string(model.TypeSeries) || typ == string(model.TypeMovie) {
			return id, nil
		}
		id = parent.String
	}
	return id, nil
}

// Children lists the direct children of an item of the given type, in index order.
func (s *Store) Children(ctx context.Context, parentID string, t model.ItemType) ([]model.Item, error) {
	items, _, err := s.ListItems(ctx, ItemQuery{ParentID: parentID, Types: []model.ItemType{t}, SortBy: "index", Limit: 10000})
	return items, err
}

// CountUnrefreshedEpisodes reports how many episodes under a series have never
// received provider metadata; the metadata service uses it to decide whether a
// recently refreshed series still needs its new episodes filled in.
func (s *Store) CountUnrefreshedEpisodes(ctx context.Context, seriesID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items e JOIN items se ON se.id = e.parent_id
		WHERE se.parent_id = ? AND e.type = 'episode' AND e.metadata_refreshed_at IS NULL`, seriesID).Scan(&n)
	return n, err
}

// TopLevelItemIDs lists every series and movie id (for the post-scan metadata sweep).
func (s *Store) TopLevelItemIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM items WHERE type IN ('series', 'movie') ORDER BY date_created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
