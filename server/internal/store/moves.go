package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Watch history follows a media file that leaves the library and comes back as a new item
// (a renamed or moved folder, a move to another library, a remux, the same film fetched
// again): its identity comes from its path or the id tag in it, and history is kept by
// identity. When a file's record is removed its departure is noted; a file that arrives
// (before or after, in any library) is matched against the departures, and the old item's
// data is carried to the new one. Matches, strongest first:
//
//  1. the same bytes: same size and extension (and duration when known);
//  2. the same file re-encoded: same name and duration, under a top item (movie or
//     series) of the same name;
//  3. the same film or episode: once the new item's movie or series is matched to a
//     provider, the same provider id (and, for an episode, season and episode number).
//
// 1 and 2 must be unambiguous (one candidate only); 3 is an identity in itself.

const (
	departureKeep = 30 * 24 * time.Hour // how long a departure waits for its file
	arrivalBefore = 7 * 24 * time.Hour  // an arrival up to this long before a departure counts
)

// ChainEntry is one level of an item and its parents (episode, season, series).
type ChainEntry struct {
	ID    string `json:"id"`
	GUID  string `json:"guid"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// ItemForPath is the item a media file is recorded under, or nil when the file is not in
// the library.
func (s *Store) ItemForPath(ctx context.Context, path string) (*model.Item, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT ms.item_id FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
		WHERE mp.path = ? LIMIT 1`, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetItem(ctx, id)
}

// HasMediaUnder reports whether a real file is recorded for the item or anything below it
// (placeholders for missing episodes do not count).
func (s *Store) HasMediaUnder(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE tree(id) AS (
			SELECT ? UNION ALL SELECT i.id FROM items i JOIN tree t ON i.parent_id = t.id)
		SELECT COUNT(*) FROM media_sources ms WHERE ms.item_id IN (SELECT id FROM tree)`, id).Scan(&n)
	return n > 0, err
}

// ItemChain is the item and its parents, nearest first.
func (s *Store) ItemChain(ctx context.Context, id string) ([]ChainEntry, error) {
	var out []ChainEntry
	for id != "" && len(out) < 8 {
		it, err := s.GetItem(ctx, id)
		if err != nil || it == nil {
			return out, err
		}
		out = append(out, ChainEntry{ID: it.ID, GUID: it.GUID, Type: string(it.Type), Title: it.Title})
		id = ""
		if it.ParentID != nil {
			id = *it.ParentID
		}
	}
	return out, nil
}

// GetItemOrMoved is GetItem for an id a client holds: an item replaced by another when its
// file moved answers with the one that replaced it.
func (s *Store) GetItemOrMoved(ctx context.Context, id string) (*model.Item, error) {
	for hops := 0; hops < 8; hops++ {
		it, err := s.GetItem(ctx, id)
		if err != nil || it != nil {
			return it, err
		}
		var next string
		err = s.db.QueryRowContext(ctx, `SELECT new_id FROM item_redirects WHERE old_id = ?`, id).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		id = next
	}
	return nil, nil
}

// CarryChain carries what people recorded about the items in from (an item and its
// parents, as ItemChain gave them before the file moved) to the item toID and its parents,
// level by level while the types match. A level stops it when it is the same identity
// (nothing moved) or when the old item still has a file (one version of a film moved, or
// an episode left its series while others stayed). True when anything was carried.
func (s *Store) CarryChain(ctx context.Context, from []ChainEntry, toID string) (bool, error) {
	to, err := s.GetItem(ctx, toID)
	if err != nil {
		return false, err
	}
	carried := false
	for _, f := range from {
		if to == nil || string(to.Type) != f.Type || to.GUID == f.GUID {
			break
		}
		if old, err := s.GetItem(ctx, f.ID); err != nil {
			return carried, err
		} else if old != nil {
			if has, err := s.HasMediaUnder(ctx, f.ID); err != nil || has {
				return carried, err
			}
		}
		if err := s.carryLevel(ctx, f, to); err != nil {
			return carried, err
		}
		carried = true
		slog.Info("moved: watch history carried over", "type", f.Type, "from", f.Title, "to", to.Title)
		if to.ParentID == nil {
			break
		}
		if to, err = s.GetItem(ctx, *to.ParentID); err != nil {
			return carried, err
		}
	}
	return carried, nil
}

// carryLevel moves every profile's data for one old item (by GUID: it outlives the item)
// to the new item, merging where the new one has some (the more recently played one's
// position, watched state and choices; plays added up; on the watchlist if either was),
// moves hand-picked artwork while the old item still exists, and records the redirect.
func (s *Store) carryLevel(ctx context.Context, from ChainEntry, to *model.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type row struct {
		pos, plays  int64
		played, fav bool
		last        string
		audio, sub  sql.NullInt64
	}
	read := func(guid string) (map[string]row, error) {
		rows, err := tx.QueryContext(ctx, `SELECT scope_id, position_ms, play_count, played, is_favorite, COALESCE(last_played_at, ''),
			audio_stream_index, subtitle_stream_index FROM user_item_data WHERE item_guid = ?`, guid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]row{}
		for rows.Next() {
			var scope string
			var r row
			var played, fav int
			if err := rows.Scan(&scope, &r.pos, &r.plays, &played, &fav, &r.last, &r.audio, &r.sub); err != nil {
				return nil, err
			}
			r.played, r.fav = played == 1, fav == 1
			out[scope] = r
		}
		return out, rows.Err()
	}
	olds, err := read(from.GUID)
	if err != nil {
		return err
	}
	news, err := read(to.GUID)
	if err != nil {
		return err
	}
	for scope, o := range olds {
		n, both := news[scope]
		if !both {
			if _, err := tx.ExecContext(ctx, `UPDATE user_item_data SET item_guid = ? WHERE scope_id = ? AND item_guid = ?`, to.GUID, scope, from.GUID); err != nil {
				return err
			}
			continue
		}
		latest, other := n, o
		if o.last > n.last { // TimeFormat is fixed width UTC: text order is time order
			latest, other = o, n
		}
		audio, sub := latest.audio, latest.sub
		if !audio.Valid {
			audio = other.audio
		}
		if !sub.Valid {
			sub = other.sub
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_item_data SET position_ms = ?, play_count = ?, played = ?, is_favorite = ?,
			last_played_at = NULLIF(?, ''), audio_stream_index = ?, subtitle_stream_index = ? WHERE scope_id = ? AND item_guid = ?`,
			latest.pos, o.plays+n.plays, boolInt(latest.played), boolInt(o.fav || n.fav), latest.last, audio, sub, scope, to.GUID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_item_data WHERE scope_id = ? AND item_guid = ?`, scope, from.GUID); err != nil {
			return err
		}
	}
	// Artwork picked by hand (locked) wins over whatever the new item fetched on its own
	// (only while the old item exists: pruning takes its artwork with it).
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ? AND locked = 0
		AND type IN (SELECT type FROM item_images WHERE item_id = ? AND locked = 1)`, to.ID, from.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE item_images SET item_id = ? WHERE item_id = ? AND locked = 1
		AND type NOT IN (SELECT type FROM item_images WHERE item_id = ?)`, to.ID, from.ID, to.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO item_redirects (old_id, new_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT(old_id) DO UPDATE SET new_id = excluded.new_id, created_at = excluded.created_at`, from.ID, to.ID, Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordDeparture notes a media file whose record is about to be removed (call it before
// DeleteSource): what it was and which items it belonged to.
func (s *Store) RecordDeparture(ctx context.Context, path string) error {
	part, err := s.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		return err
	}
	it, err := s.ItemForPath(ctx, path)
	if err != nil || it == nil || it.IsVirtual {
		return err
	}
	chain, err := s.ItemChain(ctx, it.ID)
	if err != nil || len(chain) == 0 {
		return err
	}
	topIDs := map[string]string{}
	rows, err := s.db.QueryContext(ctx, `SELECT provider, value FROM item_provider_ids WHERE item_id = ?`, chain[len(chain)-1].ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p, v string
		if err := rows.Scan(&p, &v); err != nil {
			rows.Close()
			return err
		}
		if v != "" {
			topIDs[p] = v
		}
	}
	rows.Close()
	var season, episode any
	if it.Type == model.TypeEpisode && it.IndexAired != nil && len(chain) > 1 {
		if sea, err := s.GetItem(ctx, chain[1].ID); err == nil && sea != nil && sea.IndexAired != nil {
			season, episode = *sea.IndexAired, *it.IndexAired
		}
	}
	cj, _ := json.Marshal(chain)
	ij, _ := json.Marshal(topIDs)
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	_, err = s.db.ExecContext(ctx, `INSERT INTO departures (path, ext, stem, size, duration_ms, chain, top_ids, season, episode, departed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, path, ext, strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))), part.Size, part.DurationMs,
		string(cj), string(ij), season, episode, Now())
	return err
}

// RecordDeparturesUnder is RecordDeparture for every file at or below dir.
func (s *Store) RecordDeparturesUnder(ctx context.Context, dir string) error {
	dir = strings.TrimRight(dir, `\/`)
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM media_parts WHERE `+underDir, underDirArgs(dir)...)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, p)
	}
	rows.Close()
	for _, p := range paths {
		if err := s.RecordDeparture(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

type departure struct {
	id              int64
	path, ext, stem string
	size, duration  int64
	chain           []ChainEntry
	topIDs          map[string]string
	season, episode sql.NullInt64
	departedAt      time.Time
}

func (s *Store) pendingDepartures(ctx context.Context) ([]departure, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM departures WHERE departed_at < ?`, time.Now().Add(-departureKeep).UTC().Format(TimeFormat)); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, ext, stem, size, duration_ms, chain, top_ids, season, episode, departed_at FROM departures ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []departure
	for rows.Next() {
		var d departure
		var cj, ij, at string
		if err := rows.Scan(&d.id, &d.path, &d.ext, &d.stem, &d.size, &d.duration, &cj, &ij, &d.season, &d.episode, &at); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(cj), &d.chain) != nil || len(d.chain) == 0 {
			continue
		}
		_ = json.Unmarshal([]byte(ij), &d.topIDs)
		d.departedAt = ParseTime(at)
		out = append(out, d)
	}
	return out, rows.Err()
}

// settled reports whether a departure needs nothing more: its item's identity has a file
// again (an upgrade or re-import under the same identity keeps its history by itself).
func (s *Store) settled(ctx context.Context, d departure) bool {
	it, err := s.GetItemByGUID(ctx, d.chain[0].GUID)
	if err != nil || it == nil {
		return false
	}
	has, err := s.HasMediaUnder(ctx, it.ID)
	return err == nil && has
}

func (s *Store) dropDeparture(ctx context.Context, id int64) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM departures WHERE id = ?`, id); err != nil {
		slog.Warn("moved: dropping a settled departure", "err", err)
	}
}

var reTitleNoise = regexp.MustCompile(`[\[{(][^\]})]*[\]})]|[^\p{L}\p{N}]+`)

// sameTitle compares two titles loosely: case, punctuation, and bracketed years and tags
// ("Show (2021) {tvdb-1}" against "Show") do not count.
func sameTitle(a, b string) bool {
	norm := func(s string) string {
		return strings.TrimSpace(reTitleNoise.ReplaceAllString(strings.ToLower(s), " "))
	}
	na, nb := norm(a), norm(b)
	return na != "" && na == nb
}

// SettleDepartures matches pending departures against the files in the library that
// arrived since a week before each left (rules 1 and 2), carries history for each match,
// and drops departures that are settled. Call it after records are removed and after a
// new file is added. Returns how many were carried.
func (s *Store) SettleDepartures(ctx context.Context) (int, error) {
	deps, err := s.pendingDepartures(ctx)
	if err != nil || len(deps) == 0 {
		return 0, err
	}
	oldest := deps[0].departedAt
	for _, d := range deps {
		if d.departedAt.Before(oldest) {
			oldest = d.departedAt
		}
	}
	type arrival struct {
		path, itemID string
		size, dur    int64
	}
	rows, err := s.db.QueryContext(ctx, `SELECT mp.path, ms.item_id, mp.size, COALESCE(mp.duration_ms, 0) FROM media_parts mp
		JOIN media_sources ms ON ms.id = mp.source_id WHERE ms.probed_at >= ?`, oldest.Add(-arrivalBefore).UTC().Format(TimeFormat))
	if err != nil {
		return 0, err
	}
	var arrivals []arrival
	for rows.Next() {
		var a arrival
		if err := rows.Scan(&a.path, &a.itemID, &a.size, &a.dur); err != nil {
			rows.Close()
			return 0, err
		}
		arrivals = append(arrivals, a)
	}
	rows.Close()

	carried := 0
	for _, d := range deps {
		if s.settled(ctx, d) {
			s.dropDeparture(ctx, d.id)
			continue
		}
		durOK := func(a arrival) bool {
			diff := a.dur - d.duration
			return a.dur == 0 || d.duration == 0 || (diff >= -2000 && diff <= 2000)
		}
		var bytes, recoded []arrival
		for _, a := range arrivals {
			if a.path == d.path {
				continue
			}
			name := filepath.Base(a.path)
			if a.size == d.size && strings.ToLower(filepath.Ext(name)) == d.ext && durOK(a) {
				bytes = append(bytes, a)
			}
			if strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))) == d.stem && a.dur > 0 && d.duration > 0 && durOK(a) {
				recoded = append(recoded, a)
			}
		}
		var match *arrival
		if len(bytes) == 1 {
			match = &bytes[0]
		} else if len(bytes) == 0 && len(recoded) > 0 {
			// Rule 2 also needs the top item (movie or series) to be called the same.
			var same []arrival
			for _, a := range recoded {
				chain, err := s.ItemChain(ctx, a.itemID)
				if err == nil && len(chain) > 0 && sameTitle(chain[len(chain)-1].Title, d.chain[len(d.chain)-1].Title) {
					same = append(same, a)
				}
			}
			if len(same) == 1 {
				match = &same[0]
			}
		}
		if match == nil {
			continue
		}
		ok, err := s.CarryChain(ctx, d.chain, match.itemID)
		if err != nil {
			return carried, err
		}
		if ok {
			carried++
			slog.Info("moved: file found again", "old", d.path, "new", match.path)
		}
		s.dropDeparture(ctx, d.id)
	}
	return carried, nil
}

// SettleByProvider is rule 3, for a movie or series that has just been matched to a
// provider: pending departures of the same film (same provider id), or of an episode of
// the same series with the same season and episode number, carry their history to it.
func (s *Store) SettleByProvider(ctx context.Context, topID string) (int, error) {
	top, err := s.GetItem(ctx, topID)
	if err != nil || top == nil || (top.Type != model.TypeMovie && top.Type != model.TypeSeries) {
		return 0, err
	}
	ids := map[string]string{}
	rows, err := s.db.QueryContext(ctx, `SELECT provider, value FROM item_provider_ids WHERE item_id = ?`, topID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var p, v string
		if err := rows.Scan(&p, &v); err != nil {
			rows.Close()
			return 0, err
		}
		if v != "" {
			ids[p] = v
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	deps, err := s.pendingDepartures(ctx)
	if err != nil {
		return 0, err
	}
	carried := 0
	for _, d := range deps {
		if d.chain[len(d.chain)-1].Type != string(top.Type) || d.chain[len(d.chain)-1].ID == top.ID {
			continue
		}
		same := false
		for p, v := range d.topIDs {
			if p != "nfo" && ids[p] == v {
				same = true
				break
			}
		}
		if !same {
			continue
		}
		if s.settled(ctx, d) {
			s.dropDeparture(ctx, d.id)
			continue
		}
		target := top.ID
		if top.Type == model.TypeSeries {
			if !d.season.Valid || !d.episode.Valid {
				continue
			}
			err := s.db.QueryRowContext(ctx, `SELECT e.id FROM items e JOIN items sea ON sea.id = e.parent_id
				WHERE sea.parent_id = ? AND sea.type = 'season' AND sea.index_aired = ? AND e.type = 'episode' AND e.index_aired = ?
				AND EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id) LIMIT 1`, top.ID, d.season.Int64, d.episode.Int64).Scan(&target)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return carried, err
			}
		}
		ok, err := s.CarryChain(ctx, d.chain, target)
		if err != nil {
			return carried, err
		}
		if ok {
			carried++
			slog.Info("moved: same title found again", "old", d.path, "item", top.Title)
		}
		s.dropDeparture(ctx, d.id)
	}
	return carried, nil
}
