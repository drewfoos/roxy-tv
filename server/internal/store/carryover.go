package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roxy-media/roxy/server/internal/model"
)

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

// CarryOver moves what people recorded about one item to another, for a file that moved
// (its folder renamed, the file moved to another folder, a provider id added to its name)
// and so came back as a new item: every profile's position, watched mark, play count,
// watchlist, last played time and stream choices, and artwork picked by hand. Where the
// new item already has a profile's data, the two are merged: the more recently played
// one's position and watched state and choices, plays added up, on the watchlist if
// either was. One transaction.
func (s *Store) CarryOver(ctx context.Context, fromID, toID string) error {
	if fromID == toID {
		return nil
	}
	from, err := s.GetItem(ctx, fromID)
	if err != nil || from == nil {
		return err
	}
	to, err := s.GetItem(ctx, toID)
	if err != nil || to == nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type row struct {
		scope       string
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
			var r row
			var played, fav int
			if err := rows.Scan(&r.scope, &r.pos, &r.plays, &played, &fav, &r.last, &r.audio, &r.sub); err != nil {
				return nil, err
			}
			r.played, r.fav = played == 1, fav == 1
			out[r.scope] = r
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
	// Artwork picked by hand (locked) wins over whatever the new item fetched on its own.
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ? AND locked = 0
		AND type IN (SELECT type FROM item_images WHERE item_id = ? AND locked = 1)`, to.ID, from.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE item_images SET item_id = ? WHERE item_id = ? AND locked = 1
		AND type NOT IN (SELECT type FROM item_images WHERE item_id = ?)`, to.ID, from.ID, to.ID); err != nil {
		return err
	}
	return tx.Commit()
}
