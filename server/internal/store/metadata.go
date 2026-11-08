package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ItemMetadata is what a provider lookup contributes to an item. Empty fields are left
// untouched in the database, so a thin result never erases a richer earlier one.
type ItemMetadata struct {
	Title string
	// SortTitle: nil leaves the stored sort title alone (online providers have none);
	// "" clears it; anything else sets it. A local .nfo sets it either way: it is the
	// one source of sort titles, so a <sorttitle> removed from it goes away here too.
	SortTitle       *string
	Year            *int
	Overview        string
	Tagline         string
	Genres          []string
	Advisories      []string
	CommunityRating float64 // 0 = unknown
	OfficialRating  string
	Premiered       string // YYYY-MM-DD
	Status          string
	Studio          string
	SeasonCount     int // series: 0 = unknown
	ProviderIDs     map[string]string
}

func sortTitleArg(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// UpdateItemMetadata applies provider metadata and stamps metadata_refreshed_at.
func (s *Store) UpdateItemMetadata(ctx context.Context, id string, m ItemMetadata) error {
	var genres, advisories sql.NullString
	if len(m.Genres) > 0 {
		b, _ := json.Marshal(m.Genres)
		genres = nullStr(string(b))
	}
	if len(m.Advisories) > 0 {
		b, _ := json.Marshal(m.Advisories)
		advisories = nullStr(string(b))
	}
	now := Now()
	_, err := s.db.ExecContext(ctx, `UPDATE items SET
		title = COALESCE(?, title), sort_title = CASE WHEN ? THEN NULLIF(?, '') ELSE sort_title END, year = COALESCE(?, year), overview = COALESCE(?, overview), tagline = COALESCE(?, tagline),
		genres = COALESCE(?, genres), community_rating = COALESCE(?, community_rating), official_rating = COALESCE(?, official_rating),
		premiere_date = COALESCE(?, premiere_date), status = COALESCE(?, status), studio = COALESCE(?, studio),
		season_count = COALESCE(?, season_count), advisories = COALESCE(?, advisories), metadata_refreshed_at = ?, date_modified = ? WHERE id = ?`,
		nullStr(m.Title), m.SortTitle != nil, sortTitleArg(m.SortTitle), intPtrNull(m.Year), nullStr(m.Overview), nullStr(m.Tagline), genres, nullFloat(m.CommunityRating),
		nullStr(m.OfficialRating), nullStr(m.Premiered), nullStr(m.Status), nullStr(m.Studio), nullInt(m.SeasonCount), advisories, now, now, id)
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

// SetProviderIDs upserts an item's provider ids; empty values are skipped.
func (s *Store) SetProviderIDs(ctx context.Context, itemID string, ids map[string]string) error {
	for p, v := range ids {
		if v == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO item_provider_ids (item_id, provider, value) VALUES (?,?,?)
			ON CONFLICT(item_id, provider) DO UPDATE SET value = excluded.value`, itemID, p, v); err != nil {
			return err
		}
	}
	return nil
}

// DeleteProviderIDs removes the named provider ids from an item.
func (s *Store) DeleteProviderIDs(ctx context.Context, itemID string, providers ...string) error {
	for _, p := range providers {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM item_provider_ids WHERE item_id = ? AND provider = ?`, itemID, p); err != nil {
			return err
		}
	}
	return nil
}

// ProviderID is one provider id of an item ("" when it has none).
func (s *Store) ProviderID(ctx context.Context, itemID, provider string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM item_provider_ids WHERE item_id = ? AND provider = ?`, itemID, provider).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// ShowcasePosters picks up to limit random series and movies that have a poster (the
// sign-in page's poster wall).
func (s *Store) ShowcasePosters(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id FROM items i JOIN item_images im ON im.item_id = i.id AND im.type = 'poster'
		WHERE i.type IN ('series', 'movie') AND i.is_virtual = 0 ORDER BY random() LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
	// Locked marks art the admin picked by hand; metadata refreshes leave it alone.
	Locked bool
}

func (s *Store) SetItemImage(ctx context.Context, img ItemImage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO item_images (item_id, type, path, width, height, source_url, fetched_at, locked) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(item_id, type) DO UPDATE SET path = excluded.path, width = excluded.width, height = excluded.height,
		source_url = excluded.source_url, fetched_at = excluded.fetched_at, locked = excluded.locked`,
		img.ItemID, img.Type, img.Path, nullInt(img.Width), nullInt(img.Height), nullStr(img.SourceURL), Now(), boolInt(img.Locked))
	return err
}

func (s *Store) GetItemImage(ctx context.Context, itemID, imageType string) (*ItemImage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT item_id, type, path, width, height, source_url, fetched_at, locked FROM item_images WHERE item_id = ? AND type = ?`, itemID, imageType)
	img, err := scanImage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return img, err
}

func (s *Store) ImagesForItem(ctx context.Context, itemID string) ([]ItemImage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, type, path, width, height, source_url, fetched_at, locked FROM item_images WHERE item_id = ? ORDER BY type`, itemID)
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
	var locked int
	if err := sc.Scan(&img.ItemID, &img.Type, &img.Path, &w, &h, &src, &at, &locked); err != nil {
		return nil, err
	}
	img.Locked = locked == 1
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

// EpisodesOf is every episode under a series (its seasons'), placeholders included, in
// pages so a long show is read whole: ListItems answers 100 at a time unless asked for
// more, and callers that took its default saw only the first 100 (SpongeBob's 716).
func (s *Store) EpisodesOf(ctx context.Context, seriesID string) ([]model.Item, error) {
	const page = MaxItemsUnderParent // one query: one consistent view (pages only past that)
	var out []model.Item
	for offset := 0; ; offset += page {
		items, _, err := s.ListItems(ctx, ItemQuery{ParentID: seriesID, Recursive: true, Types: []model.ItemType{model.TypeEpisode}, SortBy: "index", Limit: page, Offset: offset})
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < page {
			return out, nil
		}
	}
}

// CountUnrefreshedEpisodes reports how many real episodes under a series have never
// received provider metadata although a provider could fill them (they carry an episode
// number), counting only those created (or whose file was probed) after since when it is
// set: the metadata service
// uses it to decide whether a recently refreshed series has new episodes to fill in,
// without re-trying the ones the last attempt already could not fill.
func (s *Store) CountUnrefreshedEpisodes(ctx context.Context, seriesID string, since *time.Time) (int, error) {
	q := `SELECT COUNT(*) FROM items e JOIN items se ON se.id = e.parent_id
		WHERE se.parent_id = ? AND e.type = 'episode' AND e.is_virtual = 0 AND e.metadata_refreshed_at IS NULL
		AND COALESCE(e.index_aired, e.index_absolute) IS NOT NULL`
	args := []any{seriesID}
	if since != nil {
		// New since then: the episode, or its file (an episode made before the last
		// attempt whose file was still being read in then: that attempt left it alone).
		q += ` AND (e.date_created > ? OR EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id AND ms.probed_at > ?))`
		at := since.UTC().Format(TimeFormat)
		args = append(args, at, at)
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// MetadataAttemptedAt is when the metadata service last tried the item (nil = never).
func (s *Store) MetadataAttemptedAt(ctx context.Context, id string) (*time.Time, error) {
	var v sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT metadata_attempted_at FROM items WHERE id = ?`, id).Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t := ParseTime(v.String)
	return &t, nil
}

// SetMetadataAttempted records a finished lookup attempt that started at t.
func (s *Store) SetMetadataAttempted(ctx context.Context, id string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET metadata_attempted_at = ? WHERE id = ?`, t.UTC().Format(TimeFormat), id)
	return err
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
