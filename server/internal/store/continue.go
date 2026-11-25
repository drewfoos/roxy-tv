package store

import (
	"context"
	"database/sql"
	"sort"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/seasonlist"
)

// ContinueItems is Home's Continue Watching: one entry per show, most recent first.
// A show whose latest activity is a half-watched episode offers that episode (the
// resume list); a show whose latest activity is a finished episode offers the next
// unwatched episode after it, as Crunchyroll does, so finishing E3 brings up E4 rather
// than dropping the show. Films appear while half-watched.
func (s *Store) ContinueItems(ctx context.Context, scopeID string, limit int) ([]model.Item, error) {
	if limit <= 0 {
		limit = 20
	}
	type entry struct {
		item model.Item
		at   string
	}
	var entries []entry
	seriesAt := map[string]string{} // series id -> latest activity already represented

	// 1. Half-watched items (one episode per series), with when they were played.
	resume, err := s.ResumeItems(ctx, scopeID, limit)
	if err != nil {
		return nil, err
	}
	for _, it := range resume {
		var at, series string
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(u.last_played_at, ''), COALESCE(se.parent_id, '')
			FROM items i JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
			LEFT JOIN items se ON se.id = i.parent_id AND i.type = 'episode'
			WHERE i.id = ?`, scopeID, it.ID).Scan(&at, &series)
		entries = append(entries, entry{it, at})
		if series != "" {
			seriesAt[series] = at
		}
	}

	// 2. The latest finished episode of each series, picked per series in the query: a
	// limit over all finished episodes first let one long show (a 300-episode series
	// marked watched) fill the window and hide every other show's next episode. Episodes
	// finished at the same moment (one "mark all") give the last in order.
	rows, err := s.db.QueryContext(ctx, `SELECT series, item, season, ep, at, marked FROM (
			SELECT se.parent_id AS series, i.id AS item, se.index_aired AS season, i.index_aired AS ep,
				u.last_played_at AS at, COALESCE(u.marked_at, '') AS marked,
				ROW_NUMBER() OVER (PARTITION BY se.parent_id
					ORDER BY u.last_played_at DESC, COALESCE(se.index_aired, 0) DESC, COALESCE(i.index_aired, 0) DESC, i.id DESC) AS rn
			FROM user_item_data u
			JOIN items i ON i.guid = u.item_guid AND i.type = 'episode'
			JOIN items se ON se.id = i.parent_id
			WHERE u.scope_id = ? AND u.played = 1 AND u.last_played_at IS NOT NULL AND u.last_played_at <> ''
		) WHERE rn = 1 ORDER BY at DESC LIMIT 300`, scopeID)
	if err != nil {
		return nil, err
	}
	type finished struct {
		series, item  string
		season, epNum int
		at            string
		marked        bool // the latest thing done to it was a "mark as watched", not a viewing
	}
	var done []finished
	for rows.Next() {
		var f finished
		var season, ep *int
		var marked string
		if err := rows.Scan(&f.series, &f.item, &season, &ep, &f.at, &marked); err != nil {
			rows.Close()
			return nil, err
		}
		f.marked = marked != "" && marked >= f.at
		if season != nil {
			f.season = *season
		}
		if ep != nil {
			f.epNum = *ep
		}
		done = append(done, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. For each, the next episode not yet watched, when the finish is the series' latest activity.
	var nextUp []model.Item
	for _, f := range done {
		if at, ok := seriesAt[f.series]; ok && at >= f.at {
			continue // a half-watched episode of this show is more recent
		}
		it, err := s.nextEpisodeAfter(ctx, scopeID, f.series, f.item, true)
		if err != nil || it == nil {
			// Nothing unwatched after it. On a rewatch (every episode already watched)
			// the next one in order still comes up; otherwise this was the last there is.
			// A rewatch is a viewing: an episode that was only marked watched ("mark all
			// as watched" on a series) is not one, and used to bring up an episode the
			// profile had just marked.
			if f.marked {
				continue
			}
			if all, err2 := s.allEpisodesPlayed(ctx, scopeID, f.series); err2 == nil && all {
				it, err = s.nextEpisodeAfter(ctx, scopeID, f.series, f.item, false)
			}
			if err != nil || it == nil {
				continue
			}
		}
		// Replaces an older half-watched episode of the same show.
		if _, ok := seriesAt[f.series]; ok {
			for i := range entries {
				var series string
				_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(se.parent_id, '') FROM items i LEFT JOIN items se ON se.id = i.parent_id WHERE i.id = ?`, entries[i].item.ID).Scan(&series)
				if series == f.series {
					entries = append(entries[:i], entries[i+1:]...)
					break
				}
			}
		}
		seriesAt[f.series] = f.at
		entries = append(entries, entry{*it, f.at})
		nextUp = append(nextUp, *it)
	}
	if len(nextUp) > 0 {
		if err := s.fillItemExtras(ctx, nextUp); err != nil {
			return nil, err
		}
		filled := map[string]model.Item{}
		for _, it := range nextUp {
			filled[it.ID] = it
		}
		for i := range entries {
			if it, ok := filled[entries[i].item.ID]; ok {
				entries[i].item = it
			}
		}
	}
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].at > entries[b].at })
	out := make([]model.Item, 0, len(entries))
	for _, e := range entries {
		if len(out) == limit {
			break
		}
		out = append(out, e.item)
	}
	return out, nil
}

// seriesEpisodes joins a series' seasons (se) to their episodes (i), both found by
// parent. Left to SQLite, "the next episode" read every episode in the library for each
// show on Continue Watching (it starts from the type index), one such read per show.
const seriesEpisodes = `items se INDEXED BY items_parent JOIN items i INDEXED BY items_parent ON i.parent_id = se.id`

// watchEpisode is one episode of a series as the order of watching needs it.
type watchEpisode struct {
	seasonlist.Episode
	played bool
}

// watchEpisodes reads every episode of a series (placeholders included: a season starts
// when its first episode came out, held or not), with whether the profile watched it
// ("" for nobody's).
func (s *Store) watchEpisodes(ctx context.Context, scopeID, seriesID string) ([]watchEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, se.id, se.index_aired, COALESCE(i.index_aired, i.index_absolute, 0),
		COALESCE(i.title, ''), COALESCE(i.premiere_date, ''), COALESCE(i.runtime_ms, 0), i.is_virtual,
		i.airs_before_season, i.airs_before_episode, i.airs_after_season, COALESCE(u.played, 0)
		FROM `+seriesEpisodes+`
		LEFT JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE se.parent_id = ? AND i.type = 'episode'`, scopeID, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []watchEpisode
	for rows.Next() {
		var e watchEpisode
		var season, bs, be, as sql.NullInt64
		var virtual, played int
		if err := rows.Scan(&e.ID, &e.SeasonID, &season, &e.Number, &e.Title, &e.Date, &e.RuntimeMs, &virtual, &bs, &be, &as, &played); err != nil {
			return nil, err
		}
		e.Season = seasonlist.Unnumbered
		if season.Valid {
			e.Season = int(season.Int64)
		}
		e.Virtual, e.played = virtual == 1, played == 1
		e.BeforeSeason, e.BeforeEpisode, e.AfterSeason = ptrInt(bs), ptrInt(be), ptrInt(as)
		out = append(out, e)
	}
	return out, rows.Err()
}

func watchKeys(eps []watchEpisode) map[string]seasonlist.Key {
	plain := make([]seasonlist.Episode, len(eps))
	for i := range eps {
		plain[i] = eps[i].Episode
	}
	return seasonlist.Keys(plain)
}

// WatchKeys gives every episode of a series its place in the order the series is
// watched in: the season list read from the top (seasonlist.Keys).
func (s *Store) WatchKeys(ctx context.Context, seriesID string) (map[string]seasonlist.Key, error) {
	eps, err := s.watchEpisodes(ctx, "", seriesID)
	if err != nil {
		return nil, err
	}
	return watchKeys(eps), nil
}

// nextEpisodeAfter is the first real episode of a series after the one just finished
// (afterID) in the order of watching: the season list read from the top, so the OVAs
// that came out between two seasons come between them (seasonlist.Keys; it went by the
// seasons' numbers before, and after a season's last episode came the next season's
// first). With unplayedOnly, the first the profile has not watched. The extras at the
// list's foot never come up; after one of them, what is next is counted from the top.
func (s *Store) nextEpisodeAfter(ctx context.Context, scopeID, seriesID, afterID string, unplayedOnly bool) (*model.Item, error) {
	eps, err := s.watchEpisodes(ctx, scopeID, seriesID)
	if err != nil {
		return nil, err
	}
	keys := watchKeys(eps)
	after := int64(-1)
	if k, ok := keys[afterID]; ok && !k.Extra {
		after = k.Order
	}
	best, bestAt := "", int64(0)
	for _, e := range eps {
		k := keys[e.ID]
		if e.ID == afterID || e.Virtual || k.Extra || k.Order <= after || (unplayedOnly && e.played) {
			continue
		}
		if best == "" || k.Order < bestAt || (k.Order == bestAt && e.ID < best) {
			best, bestAt = e.ID, k.Order
		}
	}
	if best == "" {
		return nil, nil
	}
	return s.GetItem(ctx, best)
}

// allEpisodesPlayed reports whether the profile has watched every real regular episode
// of a series (so finishing one again is a rewatch).
func (s *Store) allEpisodesPlayed(ctx context.Context, scopeID, seriesID string) (bool, error) {
	var unplayed int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+seriesEpisodes+`
		LEFT JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE se.parent_id = ? AND i.type = 'episode' AND i.is_virtual = 0
		  AND COALESCE(se.index_aired, 0) > 0 AND COALESCE(u.played, 0) = 0`, scopeID, seriesID).Scan(&unplayed)
	return unplayed == 0, err
}
