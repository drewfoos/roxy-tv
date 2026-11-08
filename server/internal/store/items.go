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
	"season_count", "related_count", "advisories", "index_aired_end", "airs_before_season", "airs_before_episode", "airs_after_season", "extra_type"}

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
	var parent, sortTitle, overview, tagline, genres, official, premiered, status, studio, refreshed, advisories, extraType sql.NullString
	var year, idxAired, seasonAired, idxAbs, runtime, seasonCount, relatedCount, idxEnd, beforeSeason, beforeEp, afterSeason sql.NullInt64
	var rating sql.NullFloat64
	var created, modified string
	var virtual int
	dest := []any{&it.ID, &it.GUID, &it.LibraryID, &it.Type, &parent, &it.Title, &sortTitle, &year, &overview,
		&idxAired, &seasonAired, &idxAbs, &runtime, &created, &modified,
		&tagline, &genres, &rating, &official, &premiered, &status, &studio, &refreshed, &virtual, &seasonCount, &relatedCount, &advisories, &idxEnd, &beforeSeason, &beforeEp, &afterSeason, &extraType}
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
	it.IndexAiredEnd = ptrInt(idxEnd)
	it.AirsBeforeSeason, it.AirsBeforeEpisode, it.AirsAfterSeason = ptrInt(beforeSeason), ptrInt(beforeEp), ptrInt(afterSeason)
	it.ExtraType = extraType.String
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
	// An extra with a file and no picture (no provider has one of a trailer, an opening, a
	// short) offers a thumb anyway: asked for, it is made from a frame of the file
	// (api.getItemImage, stills.Grab). Clients only ask for the images an item lists, so
	// extras' rows used to have none.
	for i := range items {
		if items[i].Type == model.TypeExtra && items[i].HasMedia && items[i].Images["thumb"] == "" {
			if items[i].Images == nil {
				items[i].Images = map[string]string{}
			}
			items[i].Images["thumb"] = ImageURL(items[i].ID, "thumb")
		}
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
// The most items one /v1/items request returns: whole lists come in one query (one
// consistent view) up to this. With their media sources (include=sources: files and
// tracks, about four times the JSON) the API caps lower (MaxItemsWithSources): 20000 of
// those were 36 MB and 600 MB of allocations, too much for a TV to take in at once.
const (
	MaxItems            = 20000
	MaxItemsWithSources = 5000
)

// ItemsVersion counts every change to items (migration 0042): a list read in pages
// compares it between pages to know it was read from one state.
func (s *Store) ItemsVersion(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT n FROM items_version WHERE id = 1`).Scan(&n)
	return n, err
}

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
	} else {
		// Extras are listed only when asked for (ItemsExtras); never with a title's
		// children or a library's items.
		where = append(where, "type <> 'extra'")
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
	// Bounded so one request can't make the server load and send the whole database; up to
	// MaxItems a list is read whole in one query (one consistent view: pages could skip an
	// item a scan moved between them).
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > MaxItems {
		limit = MaxItems
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	// id last: items equal on the sort (two files of one episode number, unnumbered ones,
	// a scan's batch created in the same millisecond) otherwise came back in any order,
	// and a page boundary between them repeated one item and skipped another.
	query := fmt.Sprintf(`SELECT %s FROM items%s ORDER BY %s%s, id%s LIMIT %d OFFSET %d`, itemCols, sqlWhere, order, dir, dir, limit, q.Offset)
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
// The sort title is only replaced by a non-empty one: the scanner never supplies one, and
// a rescan used to clear whatever was set.
//
// A placeholder that becomes real (its file arrived) is the exception: what it showed
// was the provider's guess for that slot, which need not be what the file is (an OVA
// filed as a season's last episode), so it starts over from the file, without the
// provider still (its air date too), as a newly added episode; the next metadata pass
// fills it again.
func (s *Store) UpsertItem(ctx context.Context, it *model.Item) error {
	now := Now()
	existing, err := s.GetItemByGUID(ctx, it.GUID)
	if err != nil {
		return err
	}
	if existing != nil {
		it.ID = existing.ID
		it.DateCreated = existing.DateCreated
		if existing.IsVirtual && !it.IsVirtual {
			if _, err := s.db.ExecContext(ctx, `UPDATE items SET metadata_refreshed_at = NULL, overview = NULL, premiere_date = NULL, date_created = ? WHERE id = ?`, now, it.ID); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ? AND type = 'thumb' AND locked = 0`, it.ID); err != nil {
				return err
			}
			it.DateCreated = ParseTime(now)
		}
		_, err = s.db.ExecContext(ctx, `UPDATE items SET library_id=?, type=?, parent_id=?,
			title = CASE WHEN metadata_refreshed_at IS NULL THEN ? ELSE title END,
			sort_title = COALESCE(?, sort_title),
			year = CASE WHEN metadata_refreshed_at IS NULL THEN ? ELSE COALESCE(year, ?) END,
			overview = COALESCE(overview, ?),
			index_aired=?, season_aired=?, index_absolute=?, index_aired_end=?, runtime_ms=COALESCE(?, runtime_ms), is_virtual=?,
			extra_type=COALESCE(?, extra_type), date_modified=? WHERE id=?`,
			it.LibraryID, it.Type, strPtrNull(it.ParentID), it.Title, nullStr(it.SortTitle), intPtrNull(it.Year), intPtrNull(it.Year), nullStr(it.Overview),
			intPtrNull(it.IndexAired), intPtrNull(it.SeasonAired), intPtrNull(it.IndexAbsolute), intPtrNull(it.IndexAiredEnd), int64PtrNull(it.RuntimeMs), boolInt(it.IsVirtual),
			nullStr(it.ExtraType), now, it.ID)
	} else {
		if it.ID == "" {
			return errors.New("UpsertItem: new item needs an ID")
		}
		it.DateCreated = ParseTime(now)
		_, err = s.db.ExecContext(ctx, `INSERT INTO items (id, guid, library_id, type, parent_id, title, sort_title, year, overview,
			index_aired, season_aired, index_absolute, index_aired_end, runtime_ms, is_virtual, extra_type, date_created, date_modified) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.GUID, it.LibraryID, it.Type, strPtrNull(it.ParentID), it.Title, nullStr(it.SortTitle), intPtrNull(it.Year), nullStr(it.Overview),
			intPtrNull(it.IndexAired), intPtrNull(it.SeasonAired), intPtrNull(it.IndexAbsolute), intPtrNull(it.IndexAiredEnd), int64PtrNull(it.RuntimeMs), boolInt(it.IsVirtual),
			nullStr(it.ExtraType), now, now)
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
// created. Placeholders never overwrite anything, except that an existing placeholder
// follows the library id it is given (its series' library: a series that moved takes
// its placeholders along).
func (s *Store) EnsureVirtualItem(ctx context.Context, it *model.Item) (*model.Item, bool, error) {
	existing, err := s.GetItemByGUID(ctx, it.GUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if existing.IsVirtual && it.LibraryID != 0 && existing.LibraryID != it.LibraryID {
			if _, err := s.db.ExecContext(ctx, `UPDATE items SET library_id = ? WHERE id = ? AND is_virtual = 1`, it.LibraryID, existing.ID); err != nil {
				return nil, false, err
			}
			existing.LibraryID = it.LibraryID
		}
		return existing, false, nil
	}
	it.IsVirtual = true
	if err := s.UpsertItem(ctx, it); err != nil {
		return nil, false, err
	}
	return it, true, nil
}

// SyncChildLibrary gives every season and episode under a series the series' library id
// (placeholders included). Returns how many rows changed.
func (s *Store) SyncChildLibrary(ctx context.Context, seriesID string) (int, error) {
	total := 0
	for _, q := range []string{
		`UPDATE items SET library_id = (SELECT library_id FROM items WHERE id = ?) WHERE parent_id = ? AND type = 'season'
			AND library_id != (SELECT library_id FROM items WHERE id = ?)`,
		`UPDATE items SET library_id = (SELECT library_id FROM items WHERE id = ?) WHERE type = 'episode'
			AND parent_id IN (SELECT id FROM items WHERE parent_id = ?) AND library_id != (SELECT library_id FROM items WHERE id = ?)`,
	} {
		res, err := s.db.ExecContext(ctx, q, seriesID, seriesID, seriesID)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, nil
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

// shadowedPlaceholders matches placeholder episodes that a real episode in the same
// season already covers: same episode number, counting an absolute number as the
// episode number when the file has no season numbering (a season-folder show named
// "Show - 03.mkv" gets the GUID abs0003, the placeholder s01e0003, so the GUIDs never
// meet and both rows would show), or a number inside a multi-episode file's range
// (S01E01-E03 covers E2 and E3).
const shadowedPlaceholders = `is_virtual = 1 AND type = 'episode' AND EXISTS (
	SELECT 1 FROM items r WHERE r.is_virtual = 0 AND r.type = 'episode' AND r.parent_id = items.parent_id
	AND items.index_aired BETWEEN COALESCE(r.index_aired, r.index_absolute)
		AND MAX(COALESCE(r.index_aired, r.index_absolute), COALESCE(r.index_aired_end, 0)))`

// SetIndexEnd records the last episode a multi-episode file covers (nil: a single one).
func (s *Store) SetIndexEnd(ctx context.Context, itemID string, end *int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET index_aired_end = ? WHERE id = ?`, intPtrNull(end), itemID)
	return err
}

// DeleteShadowedPlaceholders drops placeholder episodes that a real file in the same
// season already stands for (see shadowedPlaceholders); seriesID limits it to one show,
// "" means the whole library.
func (s *Store) DeleteShadowedPlaceholders(ctx context.Context, seriesID string) (int, error) {
	q := `DELETE FROM items WHERE ` + shadowedPlaceholders
	var args []any
	if seriesID != "" {
		q += ` AND parent_id IN (SELECT id FROM items WHERE parent_id = ?)`
		args = append(args, seriesID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PruneOrphans removes real (non-virtual) episodes and movies whose media vanished, then
// seasons and series left with no children, then placeholder episodes a real file now
// covers. Other placeholders are kept: a season of only missing episodes is still worth
// showing.
func (s *Store) PruneOrphans(ctx context.Context) (int, error) {
	total := 0
	for _, q := range []string{
		`DELETE FROM items WHERE type IN ('episode','movie','extra') AND is_virtual = 0 AND NOT EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = items.id)`,
		`DELETE FROM items WHERE ` + shadowedPlaceholders,
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

// Extras lists the extras of a film or show (trailers, featurettes, ...), by kind then
// title, with their files.
func (s *Store) Extras(ctx context.Context, ownerID string) ([]model.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM items WHERE parent_id = ? AND type = 'extra'
		ORDER BY CASE extra_type WHEN 'trailer' THEN 0 ELSE 1 END, extra_type, title COLLATE NOCASE`, ownerID)
	if err != nil {
		return nil, err
	}
	var out []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.fillItemExtras(ctx, out)
}

// ItemsWithMediaUnder lists the titles (top-level items: movies and series) whose files
// sit at or below dir, with the file paths, extras left out.
func (s *Store) ItemsWithMediaUnder(ctx context.Context, dir string) (map[string]string, error) {
	dir = strings.TrimRight(dir, `\/`)
	rows, err := s.db.QueryContext(ctx, `SELECT mp.path, ms.item_id FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
		JOIN items i ON i.id = ms.item_id WHERE i.type <> 'extra' AND `+strings.ReplaceAll(underDir, "path", "mp.path"), underDirArgs(dir)...)
	if err != nil {
		return nil, err
	}
	out := map[string]string{} // path -> item id
	for rows.Next() {
		var p, id string
		if err := rows.Scan(&p, &id); err != nil {
			rows.Close()
			return nil, err
		}
		out[p] = id
	}
	rows.Close()
	return out, rows.Err()
}

// SetAirDateIfUnset records an episode's air date when it has none (a file named by its
// date), without marking its metadata as looked up.
func (s *Store) SetAirDateIfUnset(ctx context.Context, id, date string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET premiere_date = ? WHERE id = ? AND (premiere_date IS NULL OR premiere_date = '')`, date, id)
	return err
}

// SetSpecialPlacement records where a special belongs in story order (nil clears each).
func (s *Store) SetSpecialPlacement(ctx context.Context, id string, beforeSeason, beforeEpisode, afterSeason *int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET airs_before_season = ?, airs_before_episode = ?, airs_after_season = ? WHERE id = ?`,
		intPtrNull(beforeSeason), intPtrNull(beforeEpisode), intPtrNull(afterSeason), id)
	return err
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

// ResetEpisodeToFile puts an episode back on what its file says: the file's title, no
// provider overview, air date or still (frame grabs and artwork next to the media stay).
// Used when an earlier lookup gave it another episode's data.
func (s *Store) ResetEpisodeToFile(ctx context.Context, id, title string) error {
	now := Now()
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET title = ?, overview = NULL, premiere_date = NULL, metadata_refreshed_at = ?, date_modified = ?
		WHERE id = ? AND type = 'episode'`, title, now, now, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ? AND type = 'thumb' AND locked = 0 AND source_url LIKE 'http%'`, id)
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
