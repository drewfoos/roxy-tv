package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// itemColNames lists the item columns scanItem reads, in order. itemCols is the
// unqualified SELECT list; itemColsFor(alias) qualifies them for joins.
var itemColNames = []string{"id", "guid", "library_id", "type", "parent_id", "title", "sort_title", "year", "overview",
	"index_aired", "season_aired", "index_absolute", "runtime_ms", "date_created", "date_modified",
	"tagline", "genres", "community_rating", "official_rating", "premiere_date", "status", "studio", "metadata_refreshed_at", "is_virtual",
	"season_count", "related_count", "advisories"}

var itemCols = strings.Join(itemColNames, ", ")

func itemColsFor(alias string) string {
	q := make([]string, len(itemColNames))
	for i, c := range itemColNames {
		q[i] = alias + "." + c
	}
	return strings.Join(q, ", ")
}

// scanItem reads one item row; extra receives any columns selected after itemCols.
func scanItem(sc interface{ Scan(...any) error }, extra ...any) (*model.Item, error) {
	var it model.Item
	var parent, sortTitle, overview, tagline, genres, official, premiered, status, studio, refreshed, advisories sql.NullString
	var year, idxAired, seasonAired, idxAbs, runtime, seasonCount, relatedCount sql.NullInt64
	var rating sql.NullFloat64
	var created, modified string
	var virtual int
	dest := []any{&it.ID, &it.GUID, &it.LibraryID, &it.Type, &parent, &it.Title, &sortTitle, &year, &overview,
		&idxAired, &seasonAired, &idxAbs, &runtime, &created, &modified,
		&tagline, &genres, &rating, &official, &premiered, &status, &studio, &refreshed, &virtual, &seasonCount, &relatedCount, &advisories}
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	it.ParentID = ptrStr(parent)
	it.SortTitle = sortTitle.String
	it.Overview = overview.String
	it.Year = ptrInt(year)
	it.IndexAired = ptrInt(idxAired)
	it.SeasonAired = ptrInt(seasonAired)
	it.IndexAbsolute = ptrInt(idxAbs)
	it.RuntimeMs = ptrInt64(runtime)
	it.DateCreated = ParseTime(created)
	it.DateModified = ParseTime(modified)
	it.Tagline = tagline.String
	if genres.Valid && genres.String != "" {
		_ = json.Unmarshal([]byte(genres.String), &it.Genres)
	}
	if advisories.Valid && advisories.String != "" {
		_ = json.Unmarshal([]byte(advisories.String), &it.Advisories)
	}
	if rating.Valid {
		r := rating.Float64
		it.CommunityRating = &r
	}
	it.OfficialRating = official.String
	it.Premiered = premiered.String
	if it.Type == model.TypeEpisode {
		it.AirDate = premiered.String
	}
	it.Status = status.String
	it.Studio = studio.String
	it.IsVirtual = virtual == 1
	it.SeasonCount = ptrInt(seasonCount)
	it.RelatedCount = int(relatedCount.Int64)
	if refreshed.Valid {
		if t := ParseTime(refreshed.String); !t.IsZero() {
			it.MetadataRefreshedAt = &t
		}
	}
	return &it, nil
}

func (s *Store) GetItem(ctx context.Context, id string) (*model.Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM items WHERE id = ?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := []model.Item{*it}
	if err := s.fillItemExtras(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *Store) GetItemByGUID(ctx context.Context, guid string) (*model.Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM items WHERE guid = ?`, guid)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

// fillItemExtras attaches provider ids and image URLs to a batch of items with one
// query per table (the store has a single connection, so N+1 would also serialise).
func (s *Store) fillItemExtras(ctx context.Context, items []model.Item) error {
	if len(items) == 0 {
		return nil
	}
	byID := map[string][]int{}
	ids := make([]string, 0, len(items))
	for i := range items {
		if _, seen := byID[items[i].ID]; !seen {
			ids = append(ids, items[i].ID)
		}
		byID[items[i].ID] = append(byID[items[i].ID], i)
	}
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		ph := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}

		rows, err := s.db.QueryContext(ctx, `SELECT item_id, provider, value FROM item_provider_ids WHERE item_id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, p, v string
			if err := rows.Scan(&id, &p, &v); err != nil {
				rows.Close()
				return err
			}
			for _, i := range byID[id] {
				if items[i].ProviderIDs == nil {
					items[i].ProviderIDs = map[string]string{}
				}
				items[i].ProviderIDs[p] = v
			}
		}
		rows.Close()

		rows, err = s.db.QueryContext(ctx, `SELECT item_id, type, fetched_at FROM item_images WHERE item_id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, t, fetched string
			if err := rows.Scan(&id, &t, &fetched); err != nil {
				rows.Close()
				return err
			}
			for _, i := range byID[id] {
				if items[i].Images == nil {
					items[i].Images = map[string]string{}
				}
				items[i].Images[t] = ImageURLAt(id, t, fetched)
			}
		}
		rows.Close()

		rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT item_id FROM media_sources WHERE item_id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			for _, i := range byID[id] {
				items[i].HasMedia = true
			}
		}
		rows.Close()
	}
	return nil
}

// ImageURL is the relative URL the native API serves an item image at.
func ImageURL(itemID, imageType string) string {
	return "/v1/items/" + itemID + "/images/" + imageType
}

// ImageURLAt is ImageURL with a version tag from the fetch time, so a client's image
// cache (keyed by URL) drops the old file when the art is replaced.
func ImageURLAt(itemID, imageType, fetchedAt string) string {
	u := ImageURL(itemID, imageType)
	if t := ParseTime(fetchedAt); !t.IsZero() {
		u += "?v=" + strconv.FormatInt(t.Unix(), 36)
	}
	return u
}

// ItemQuery is the filter for ListItems. Zero values mean "no filter".
type ItemQuery struct {
	LibraryID int64
	Types     []model.ItemType
	ParentID  string
	Recursive bool   // when ParentID is set: include all descendants
	SortBy    string // "date_created" (default), "title", "index"
	Desc      bool
	Limit     int
	Offset    int
	// HasMedia filters on whether a media source exists (nil = no filter). Series and
	// seasons never have sources themselves; use it for episodes and movies.
	HasMedia *bool
	// FavoriteOf restricts to items this user marked as favourite (the watchlist).
	FavoriteOf string
}

func (s *Store) ListItems(ctx context.Context, q ItemQuery) ([]model.Item, int, error) {
	var where []string
	var args []any
	if q.LibraryID != 0 {
		where = append(where, "library_id = ?")
		args = append(args, q.LibraryID)
	}
	if len(q.Types) > 0 {
		ph := make([]string, len(q.Types))
		for i, t := range q.Types {
			ph[i] = "?"
			args = append(args, string(t))
		}
		where = append(where, "type IN ("+strings.Join(ph, ",")+")")
	}
	if q.ParentID != "" {
		if q.Recursive {
			// Up to three levels: series -> season -> episode.
			where = append(where, `(parent_id = ? OR parent_id IN (SELECT id FROM items WHERE parent_id = ?) OR parent_id IN (SELECT id FROM items WHERE parent_id IN (SELECT id FROM items WHERE parent_id = ?)))`)
			args = append(args, q.ParentID, q.ParentID, q.ParentID)
		} else {
			where = append(where, "parent_id = ?")
			args = append(args, q.ParentID)
		}
	}
	if q.HasMedia != nil {
		if *q.HasMedia {
			where = append(where, "EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = items.id)")
		} else {
			where = append(where, "NOT EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = items.id)")
		}
	}
	if q.FavoriteOf != "" {
		where = append(where, "EXISTS (SELECT 1 FROM user_item_data u WHERE u.item_guid = items.guid AND u.scope_id = ? AND u.is_favorite = 1)")
		args = append(args, q.FavoriteOf)
	}
	sqlWhere := ""
	if len(where) > 0 {
		sqlWhere = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`+sqlWhere, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "date_created"
	switch q.SortBy {
	case "title":
		order = "COALESCE(sort_title, title) COLLATE NOCASE"
	case "index":
		order = "season_aired, index_aired, index_absolute"
	}
	dir := " ASC"
	if q.Desc {
		dir = " DESC"
	}
	// Bounded so one request can't make the server load and send the whole database.
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > 2000 {
		limit = 2000
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	query := fmt.Sprintf(`SELECT %s FROM items%s ORDER BY %s%s LIMIT %d OFFSET %d`, itemCols, sqlWhere, order, dir, limit, q.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if err := s.fillItemExtras(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpsertItem inserts or updates by GUID and returns the stored item (with its ID).
//
// On update, fields the metadata service owns (title, year, overview) are kept once the
// item has been matched (metadata_refreshed_at set): a rescan must not undo a lookup.
func (s *Store) UpsertItem(ctx context.Context, it *model.Item) error {
	now := Now()
	existing, err := s.GetItemByGUID(ctx, it.GUID)
	if err != nil {
		return err
	}
	if existing != nil {
		it.ID = existing.ID
		it.DateCreated = existing.DateCreated
		_, err = s.db.ExecContext(ctx, `UPDATE items SET library_id=?, type=?, parent_id=?,
			title = CASE WHEN metadata_refreshed_at IS NULL THEN ? ELSE title END,
			sort_title=?,
			year = CASE WHEN metadata_refreshed_at IS NULL THEN ? ELSE COALESCE(year, ?) END,
			overview = COALESCE(overview, ?),
			index_aired=?, season_aired=?, index_absolute=?, runtime_ms=COALESCE(?, runtime_ms), is_virtual=?, date_modified=? WHERE id=?`,
			it.LibraryID, it.Type, strPtrNull(it.ParentID), it.Title, nullStr(it.SortTitle), intPtrNull(it.Year), intPtrNull(it.Year), nullStr(it.Overview),
			intPtrNull(it.IndexAired), intPtrNull(it.SeasonAired), intPtrNull(it.IndexAbsolute), int64PtrNull(it.RuntimeMs), boolInt(it.IsVirtual), now, it.ID)
	} else {
		if it.ID == "" {
			return errors.New("UpsertItem: new item needs an ID")
		}
		it.DateCreated = ParseTime(now)
		_, err = s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, parent_id, title, sort_title, year, overview,
			index_aired, season_aired, index_absolute, runtime_ms, is_virtual, date_created, date_modified) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.GUID, it.LibraryID, it.Type, strPtrNull(it.ParentID), it.Title, nullStr(it.SortTitle), intPtrNull(it.Year), nullStr(it.Overview),
			intPtrNull(it.IndexAired), intPtrNull(it.SeasonAired), intPtrNull(it.IndexAbsolute), int64PtrNull(it.RuntimeMs), boolInt(it.IsVirtual), now, now)
		if err == nil {
			_, err = s.db.ExecContext(ctx, `INSERT INTO items_fts (rowid, title) SELECT rowid, title FROM items WHERE id = ?`, it.ID)
		}
	}
	if err != nil {
		return err
	}
	it.DateModified = ParseTime(now)
	for p, v := range it.ProviderIDs {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO item_provider_ids (item_id, provider, value) VALUES (?,?,?)
			ON CONFLICT(item_id, provider) DO UPDATE SET value = excluded.value`, it.ID, p, v); err != nil {
			return err
		}
	}
	return nil
}

// EnsureVirtualItem inserts a placeholder item unless an item with the same GUID already
// exists (real or virtual). It returns the stored item either way and whether it was
// created. Placeholders never overwrite anything.
func (s *Store) EnsureVirtualItem(ctx context.Context, it *model.Item) (*model.Item, bool, error) {
	existing, err := s.GetItemByGUID(ctx, it.GUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	it.IsVirtual = true
	if err := s.UpsertItem(ctx, it); err != nil {
		return nil, false, err
	}
	return it, true, nil
}

// DeleteVirtualChildren removes placeholder episodes/seasons under a series that the
// providers no longer list (e.g. after a provider correction). Real items are untouched.
func (s *Store) DeleteVirtualChildren(ctx context.Context, seriesID string, keepGUIDs map[string]bool) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, guid FROM items WHERE is_virtual = 1 AND (parent_id = ? OR parent_id IN (SELECT id FROM items WHERE parent_id = ?))`, seriesID, seriesID)
	if err != nil {
		return 0, err
	}
	var drop []string
	for rows.Next() {
		var id, guid string
		if err := rows.Scan(&id, &guid); err != nil {
			rows.Close()
			return 0, err
		}
		if !keepGUIDs[guid] {
			drop = append(drop, id)
		}
	}
	rows.Close()
	for _, id := range drop {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ? AND is_virtual = 1 AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id AND c.is_virtual = 0)`, id); err != nil {
			return 0, err
		}
	}
	return len(drop), nil
}

// PruneOrphans removes real (non-virtual) episodes and movies whose media vanished, then
// seasons and series left with no children. Placeholders are kept: a season of only
// missing episodes is still worth showing.
func (s *Store) PruneOrphans(ctx context.Context) (int, error) {
	total := 0
	for _, q := range []string{
		`DELETE FROM items WHERE type IN ('episode','movie') AND is_virtual = 0 AND NOT EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = items.id)`,
		`DELETE FROM items WHERE type = 'season' AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`,
		`DELETE FROM items WHERE type = 'series' AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`,
	} {
		res, err := s.db.ExecContext(ctx, q)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, nil
}

// ResetEpisodeMetadata clears provider-sourced episode fields under a season (title back
// to "Episode N", overview/air date/thumb dropped) so a corrected provider can refill
// them. Used on forced refresh when a season's earlier data is known to be wrong.
func (s *Store) ResetEpisodeMetadata(ctx context.Context, seasonID string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET title = 'Episode ' || COALESCE(index_aired, index_absolute, 0), overview = NULL,
		premiere_date = NULL, metadata_refreshed_at = NULL WHERE parent_id = ? AND type = 'episode'`, seasonID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM item_images WHERE type = 'thumb' AND item_id IN (SELECT id FROM items WHERE parent_id = ? AND type = 'episode')`, seasonID)
	return err
}

func (s *Store) DeleteItem(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	return err
}

// SeriesRef is an episode's grandparent, resolved for list views.
type SeriesRef struct {
	ID    string
	Title string
}

// SeriesForSeasons maps season ids to their series in one query.
func (s *Store) SeriesForSeasons(ctx context.Context, seasonIDs []string) (map[string]SeriesRef, error) {
	out := map[string]SeriesRef{}
	if len(seasonIDs) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(seasonIDs)), ",")
	args := make([]any, len(seasonIDs))
	for i, id := range seasonIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, sr.id, sr.title FROM items s JOIN items sr ON sr.id = s.parent_id WHERE s.id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var seasonID string
		var ref SeriesRef
		if err := rows.Scan(&seasonID, &ref.ID, &ref.Title); err != nil {
			return nil, err
		}
		out[seasonID] = ref
	}
	return out, rows.Err()
}

// RecentEpisodes returns the newest episodes (by date_created) with SeriesID and
// SeriesTitle filled, one query.
func (s *Store) RecentEpisodes(ctx context.Context, limit int) ([]model.Item, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsFor("e")+`, sr.id, sr.title FROM items e
		JOIN items se ON se.id = e.parent_id JOIN items sr ON sr.id = se.parent_id
		WHERE e.type = 'episode' AND EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id)
		ORDER BY e.date_created DESC, e.season_aired DESC, e.index_aired DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	var out []model.Item
	for rows.Next() {
		var seriesID, seriesTitle string
		it, err := scanItem(rows, &seriesID, &seriesTitle)
		if err != nil {
			rows.Close()
			return nil, err
		}
		it.SeriesID, it.SeriesTitle = seriesID, seriesTitle
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, s.fillItemExtras(ctx, out)
}
