package store

import (
	"context"
	"database/sql"
	"errors"
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

	// 2. The latest finished episode of each series.
	rows, err := s.db.QueryContext(ctx, `SELECT se.parent_id, se.index_aired, i.index_aired, u.last_played_at
		FROM user_item_data u
		JOIN items i ON i.guid = u.item_guid AND i.type = 'episode'
		JOIN items se ON se.id = i.parent_id
		WHERE u.scope_id = ? AND u.played = 1 AND u.last_played_at IS NOT NULL AND u.last_played_at <> ''
		ORDER BY u.last_played_at DESC LIMIT 300`, scopeID)
	if err != nil {
		return nil, err
	}
	type finished struct {
		series        string
		season, epNum int
		at            string
	}
	var done []finished
	seen := map[string]bool{}
	for rows.Next() {
		var f finished
		var season, ep *int
		if err := rows.Scan(&f.series, &season, &ep, &f.at); err != nil {
			rows.Close()
			return nil, err
		}
		if seen[f.series] {
			continue
		}
		seen[f.series] = true
		if season != nil {
			f.season = *season
		}
		if ep != nil {
			f.epNum = *ep
		}
		done = append(done, f)
	}
	rows.Close()

	// 3. For each, the next episode not yet watched, when the finish is the series' latest activity.
	var nextUp []model.Item
	for _, f := range done {
		if at, ok := seriesAt[f.series]; ok && at >= f.at {
			continue // a half-watched episode of this show is more recent
		}
		it, err := s.nextEpisodeAfter(ctx, scopeID, f.series, f.season, f.epNum, true)
		if err != nil || it == nil {
			// Nothing unwatched after it. On a rewatch (every episode already watched)
			// the next one in order still comes up; otherwise this was the last there is.
			if all, err2 := s.allEpisodesPlayed(ctx, scopeID, f.series); err2 == nil && all {
				it, err = s.nextEpisodeAfter(ctx, scopeID, f.series, f.season, f.epNum, false)
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

// nextEpisodeAfter is the first real regular episode of a series after (season, ep), in
// order; with unplayedOnly, the first the profile has not watched.
func (s *Store) nextEpisodeAfter(ctx context.Context, scopeID, seriesID string, season, ep int, unplayedOnly bool) (*model.Item, error) {
	played := ""
	if unplayedOnly {
		played = ` AND COALESCE(u.played, 0) = 0`
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+itemColsFor("i")+` FROM items i
		JOIN items se ON se.id = i.parent_id
		LEFT JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE se.parent_id = ? AND i.type = 'episode' AND i.is_virtual = 0`+played+`
		  AND COALESCE(se.index_aired, 0) > 0
		  AND (COALESCE(se.index_aired, 0) > ? OR (COALESCE(se.index_aired, 0) = ? AND COALESCE(i.index_aired, 0) > ?))
		ORDER BY COALESCE(se.index_aired, 0), COALESCE(i.index_aired, 0) LIMIT 1`,
		scopeID, seriesID, season, season, ep)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
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
