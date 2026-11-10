package store

import (
	"context"
	"database/sql"
	"sort"

	"github.com/roxy-media/roxy/server/internal/model"
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
		it, err := s.nextEpisodeAfter(ctx, scopeID, f.series, f.item, f.season, f.epNum, true)
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
				it, err = s.nextEpisodeAfter(ctx, scopeID, f.series, f.item, f.season, f.epNum, false)
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

// storyKey orders a series' episodes as they are meant to be watched: regular episodes by
// season and number, and a special with a place in story order (airs before an episode,
// before a season, or after a season) at that place. A special without one has no place
// (ok false): next-up skips it, as it always has.
func storyKey(season, ep, special int, beforeSeason, beforeEp, afterSeason *int) (key [3]int, ok bool) {
	switch {
	case season > 0:
		return [3]int{season, ep * 2, 0}, true
	case beforeSeason != nil && *beforeSeason > 0:
		e := 1
		if beforeEp != nil && *beforeEp > 0 {
			e = *beforeEp
		}
		return [3]int{*beforeSeason, e*2 - 1, special}, true
	case afterSeason != nil && *afterSeason > 0:
		return [3]int{*afterSeason, 1 << 30, special}, true
	}
	return key, false
}

func keyLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// nextEpisodeAfter is the first real episode of a series after the one just finished
// (afterID, at season/ep), in story order (storyKey: specials placed by their .nfo come in
// between); with unplayedOnly, the first the profile has not watched.
func (s *Store) nextEpisodeAfter(ctx context.Context, scopeID, seriesID, afterID string, season, ep int, unplayedOnly bool) (*model.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, COALESCE(se.index_aired, 0), COALESCE(i.index_aired, 0),
		i.airs_before_season, i.airs_before_episode, i.airs_after_season, COALESCE(u.played, 0)
		FROM items i JOIN items se ON se.id = i.parent_id
		LEFT JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE se.parent_id = ? AND i.type = 'episode' AND i.is_virtual = 0`, scopeID, seriesID)
	if err != nil {
		return nil, err
	}
	type cand struct {
		id     string
		key    [3]int
		played bool
	}
	var cands []cand
	// A special with no place in story order keeps (0, n): everything placed comes after it.
	after := [3]int{season, ep * 2, 0}
	for rows.Next() {
		var id string
		var sn, en, played int
		var bs, be, as sql.NullInt64
		if err := rows.Scan(&id, &sn, &en, &bs, &be, &as, &played); err != nil {
			rows.Close()
			return nil, err
		}
		k, ok := storyKey(sn, en, en, ptrInt(bs), ptrInt(be), ptrInt(as))
		if id == afterID && ok {
			after = k
		}
		if ok {
			cands = append(cands, cand{id, k, played == 1})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var best *cand
	for i := range cands {
		c := &cands[i]
		if c.id == afterID || !keyLess(after, c.key) || (unplayedOnly && c.played) {
			continue
		}
		if best == nil || keyLess(c.key, best.key) {
			best = c
		}
	}
	if best == nil {
		return nil, nil
	}
	return s.GetItem(ctx, best.id)
}

// allEpisodesPlayed reports whether the profile has watched every real regular episode
// of a series (so finishing one again is a rewatch).
func (s *Store) allEpisodesPlayed(ctx context.Context, scopeID, seriesID string) (bool, error) {
	var unplayed int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items i
		JOIN items se ON se.id = i.parent_id
		LEFT JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE se.parent_id = ? AND i.type = 'episode' AND i.is_virtual = 0
		  AND COALESCE(se.index_aired, 0) > 0 AND COALESCE(u.played, 0) = 0`, scopeID, seriesID).Scan(&unplayed)
	return unplayed == 0, err
}
