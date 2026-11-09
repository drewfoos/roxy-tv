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
// PathsForGUID lists the files recorded under the item with that identity.
func (s *Store) PathsForGUID(ctx context.Context, guid string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mp.path FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
		JOIN items i ON i.id = ms.item_id WHERE i.guid = ?`, guid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

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
	for i, f := range from {
		if to == nil || to.GUID == f.GUID {
			break
		}
		// The file's own item may change kind (a date-named episode an older release took
		// for a film): its history still follows. Above it, only like for like (season to
		// season, series to series).
		if string(to.Type) != f.Type && !(i == 0 && playableLeaf(f.Type) && playableLeaf(string(to.Type))) {
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

// playableLeaf is a kind of item a file belongs to and history is kept for.
func playableLeaf(t string) bool {
	return t == string(model.TypeMovie) || t == string(model.TypeEpisode)
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
		marked      string // marked watched or unwatched by hand ('' never)
		audio, sub  sql.NullInt64
	}
	// activity is the row's latest time: played, or marked watched / unwatched.
	activity := func(r row) string {
		if r.marked > r.last {
			return r.marked
		}
		return r.last
	}
	read := func(guid string) (map[string]row, error) {
		rows, err := tx.QueryContext(ctx, `SELECT scope_id, position_ms, play_count, played, is_favorite, COALESCE(last_played_at, ''),
			COALESCE(marked_at, ''), audio_stream_index, subtitle_stream_index FROM user_item_data WHERE item_guid = ?`, guid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]row{}
		for rows.Next() {
			var scope string
			var r row
			var played, fav int
			if err := rows.Scan(&scope, &r.pos, &r.plays, &played, &fav, &r.last, &r.marked, &r.audio, &r.sub); err != nil {
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
		if activity(o) > activity(n) { // TimeFormat is fixed width UTC: text order is time order
			latest, other = o, n
		}
		last, marked := o.last, o.marked
		if n.last > last {
			last = n.last
		}
		if n.marked > marked {
			marked = n.marked
		}
		audio, sub := latest.audio, latest.sub
		if !audio.Valid {
			audio = other.audio
		}
		if !sub.Valid {
			sub = other.sub
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_item_data SET position_ms = ?, play_count = ?, played = ?, is_favorite = ?,
			last_played_at = NULLIF(?, ''), marked_at = NULLIF(?, ''), audio_stream_index = ?, subtitle_stream_index = ? WHERE scope_id = ? AND item_guid = ?`,
			latest.pos, o.plays+n.plays, boolInt(latest.played), boolInt(o.fav || n.fav), last, marked, audio, sub, scope, to.GUID); err != nil {
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
	if err != nil || it == nil || it.IsVirtual || it.Type == model.TypeExtra {
		return err // an extra's file leaving needs no history carried
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
	if err != nil || !has {
		return false
	}
	s.redirectRecreated(ctx, d)
	return true
}

// redirectRecreated points the ids of a departure's items at the items now holding their
// identities, where those were made anew: a file deleted (its item pruned) and back under
// the same name keeps its history (by GUID) but gets a new id. Nothing is carried for it,
// so no redirect was written, and a client holding the old id (a download, its offline
// progress) got 404 for good and dropped what it had to send.
func (s *Store) redirectRecreated(ctx context.Context, d departure) {
	for _, e := range d.chain {
		it, err := s.GetItemByGUID(ctx, e.GUID)
		if err != nil || it == nil || it.ID == e.ID {
			continue
		}
		if old, err := s.GetItem(ctx, e.ID); err != nil || old != nil {
			continue // the old id is still an item of its own
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO item_redirects (old_id, new_id, created_at) VALUES (?, ?, ?)
			ON CONFLICT(old_id) DO UPDATE SET new_id = excluded.new_id, created_at = excluded.created_at`, e.ID, it.ID, Now()); err != nil {
			slog.Warn("moves: redirecting a recreated item", "old", e.ID, "new", it.ID, "err", err)
			continue
		}
		slog.Info("moved: old id redirected to the item made anew", "type", e.Type, "title", e.Title)
	}
}

// stillLeaving reports whether the departure's own file is still recorded as it was when
// it left: its record is about to be removed (RecordDeparture comes first, DeleteSource
// next, and a settle run by another ingest can land between them). Such a departure is
// neither settled nor matched yet; it used to be dropped as "settled" by its own file.
// A file recorded again at that path since (a re-download) is not this one.
func (s *Store) stillLeaving(ctx context.Context, d departure) bool {
	var probed string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(ms.probed_at, '') FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
		WHERE mp.path = ?`, d.path).Scan(&probed)
	return err == nil && probed <= d.departedAt.UTC().Format(TimeFormat)
}

// episodeNumbers is an episode's season and episode number, as its season and episode
// items hold them.
func (s *Store) episodeNumbers(ctx context.Context, itemID string) (season, episode int64, ok bool) {
	err := s.db.QueryRowContext(ctx, `SELECT sea.index_aired, e.index_aired FROM items e JOIN items sea ON sea.id = e.parent_id
		WHERE e.id = ? AND e.type = 'episode' AND sea.type = 'season' AND e.index_aired IS NOT NULL AND sea.index_aired IS NOT NULL`, itemID).Scan(&season, &episode)
	return season, episode, err == nil
}

// sameSeriesEpisode is the episode with a file that has the departed episode's season and
// episode number in the same series (by identity): the file was replaced by one whose
// name gives the episode another identity ("Show - 01" by "Show S01E01", a common
// upgrade), which neither its size nor its name can match.
func (s *Store) sameSeriesEpisode(ctx context.Context, d departure) string {
	top := d.chain[len(d.chain)-1]
	if !d.season.Valid || !d.episode.Valid || top.Type != string(model.TypeSeries) {
		return ""
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT e.id FROM items ser JOIN items sea ON sea.parent_id = ser.id JOIN items e ON e.parent_id = sea.id
		WHERE ser.guid = ? AND sea.type = 'season' AND sea.index_aired = ? AND e.type = 'episode' AND e.index_aired = ? AND e.guid != ?
		AND EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id) LIMIT 1`,
		top.GUID, d.season.Int64, d.episode.Int64, d.chain[0].GUID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
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
	// Extras are not arrivals: history is never carried to one (a trailer of exactly the
	// departed file's size used to take the match, and the departure was dropped).
	rows, err := s.db.QueryContext(ctx, `SELECT mp.path, ms.item_id, mp.size, COALESCE(mp.duration_ms, 0) FROM media_parts mp
		JOIN media_sources ms ON ms.id = mp.source_id JOIN items i ON i.id = ms.item_id
		WHERE ms.probed_at >= ? AND i.type != 'extra'`, oldest.Add(-arrivalBefore).UTC().Format(TimeFormat))
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
		if s.stillLeaving(ctx, d) {
			continue
		}
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
		// Rule 2 also needs the top item (movie or series) to be called the same, and an
		// episode to have the same season and episode number where both are known: a show
		// with "Season 01/Episode 01.mkv" and "Season 02/Episode 01.mkv" used to carry a
		// deleted S1E01's history to S2E01, and Season 1's to Season 2.
		var named []arrival
		for _, a := range recoded {
			chain, err := s.ItemChain(ctx, a.itemID)
			if err != nil || len(chain) == 0 || !sameTitle(chain[len(chain)-1].Title, d.chain[len(d.chain)-1].Title) {
				continue
			}
			if d.season.Valid && d.episode.Valid {
				if se, ep, ok := s.episodeNumbers(ctx, a.itemID); ok && (se != d.season.Int64 || ep != d.episode.Int64) {
					continue
				}
			}
			named = append(named, a)
		}
		var match *arrival
		switch {
		case len(bytes) == 1:
			match = &bytes[0]
		case len(bytes) > 1:
			// Several files of exactly that size (it happens): the name and length decide
			// between them, one left or none. Rule 2 used to be tried only when no size
			// matched, so such a move was never matched and its history stayed behind.
			var both []arrival
			for _, b := range bytes {
				for _, a := range named {
					if a.path == b.path {
						both = append(both, b)
					}
				}
			}
			if len(both) == 1 {
				match = &both[0]
			}
		case len(named) == 1:
			match = &named[0]
		}
		if match == nil {
			// Rule 2b: replaced in its own series by a file read as the same episode
			// under another identity.
			if id := s.sameSeriesEpisode(ctx, d); id != "" {
				match = &arrival{itemID: id, path: "(same episode of the same series)"}
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
