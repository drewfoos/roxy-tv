package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// UserItemData is per-user watch state, keyed by the item's stable GUID so re-matching
// or re-importing an item never loses history (lesson from Plex's metadata_item_settings).
type UserItemData struct {
	UserID              string
	ItemGUID            string
	PositionMs          int64
	PlayCount           int
	Played              bool
	IsFavorite          bool
	LastPlayedAt        time.Time
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
}

func (s *Store) GetUserItemData(ctx context.Context, userID, guid string) (*UserItemData, error) {
	var d UserItemData
	var played, fav int
	var last sql.NullString
	var audio, sub sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT scope_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index
		FROM user_item_data WHERE scope_id = ? AND item_guid = ?`, userID, guid).
		Scan(&d.UserID, &d.ItemGUID, &d.PositionMs, &d.PlayCount, &played, &fav, &last, &audio, &sub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Played, d.IsFavorite = played == 1, fav == 1
	d.LastPlayedAt = ParseTime(last.String)
	d.AudioStreamIndex, d.SubtitleStreamIndex = ptrInt(audio), ptrInt(sub)
	return &d, nil
}

// RewatchMinMs is how far into a watched item a new viewing must get before it counts
// as a rewatch. Past it, the item stops being watched and becomes "in progress" at the
// new position (its play count is kept; finishing it again counts another play).
// Before it, the report is a peek: the item stays watched with no resume point, so an
// episode opened by mistake doesn't lose its tick.
const RewatchMinMs = 30_000

// SetProgress records a playback position (and track selections) for a user/item.
// On a watched item it follows [RewatchMinMs]: a watched item is never also part-way,
// which used to show "Watched" in the episode list and "Resume" on Home at once.
func (s *Store) SetProgress(ctx context.Context, userID, guid string, positionMs int64, audio, sub *int) error {
	_, err := s.setProgress(ctx, userID, guid, positionMs, audio, sub, "")
	return err
}

// SetProgressAsOf is SetProgress for a report made at [at] and sent later (a phone
// replaying what it watched offline): it applies only if nothing newer was recorded for
// the item meanwhile (last_played_at not after at). The check is part of the same
// statement, so a live report from another device cannot land between a separate check
// and the write and then be wound back. False when it was not applied.
func (s *Store) SetProgressAsOf(ctx context.Context, userID, guid string, at time.Time, positionMs int64, audio, sub *int) (bool, error) {
	return s.setProgress(ctx, userID, guid, positionMs, audio, sub, asOfStamp(at))
}

// asOfStamp is the stored form of a replayed report's time, which is both what it is
// compared against and what it records as last_played_at: the activity happened then,
// not when the phone got round to sending it. Recording the arrival time instead made a
// later replay of activity that really was newer look older and get dropped. A time in
// the future (a device clock running fast) is taken as now, so it cannot outrank
// reports made after it.
func asOfStamp(at time.Time) string {
	if now := time.Now(); at.After(now) {
		at = now
	}
	return at.UTC().Format(TimeFormat)
}

// stampOr is the last_played_at a write records: the report's own time for a replay,
// now for a live report.
func stampOr(asOf string) string {
	if asOf != "" {
		return asOf
	}
	return Now()
}

// unlessNewer is the upsert condition for the AsOf writes: no activity recorded, or none
// after the report's time; activity is playing (last_played_at) and marking watched or
// unwatched (marked_at: unmarking leaves last_played_at as it was, so an older offline
// watched mark used to undo a newer "unwatched"). TimeFormat is fixed width UTC, so the
// text compares in order. An empty asOf applies always. Takes asOf three times.
const unlessNewer = ` WHERE ? = '' OR ((user_item_data.last_played_at IS NULL OR user_item_data.last_played_at = '' OR user_item_data.last_played_at <= ?)
	AND (user_item_data.marked_at IS NULL OR user_item_data.marked_at <= ?))`

func (s *Store) setProgress(ctx context.Context, userID, guid string, positionMs int64, audio, sub *int, asOf string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, last_played_at, audio_stream_index, subtitle_stream_index)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET
			position_ms = CASE WHEN user_item_data.played = 1 AND excluded.position_ms < ? THEN user_item_data.position_ms ELSE excluded.position_ms END,
			last_played_at = CASE WHEN user_item_data.played = 1 AND excluded.position_ms < ? THEN user_item_data.last_played_at ELSE excluded.last_played_at END,
			played = CASE WHEN user_item_data.played = 1 AND excluded.position_ms < ? THEN 1 ELSE 0 END,
			audio_stream_index = COALESCE(excluded.audio_stream_index, user_item_data.audio_stream_index),
			subtitle_stream_index = COALESCE(excluded.subtitle_stream_index, user_item_data.subtitle_stream_index)`+unlessNewer,
		userID, guid, positionMs, stampOr(asOf), intPtrNull(audio), intPtrNull(sub), RewatchMinMs, RewatchMinMs, RewatchMinMs, asOf, asOf, asOf)
	return applied(res, err)
}

func applied(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FinishPlay records reaching the end of an item during playback. A player keeps
// reporting its position after the played threshold (every pause, every few seconds,
// the credits), so only the first report of a viewing counts as a play: one already
// marked played and back at position 0 is left alone.
func (s *Store) FinishPlay(ctx context.Context, userID, guid string) error {
	_, err := s.finishPlay(ctx, userID, guid, "")
	return err
}

// FinishPlayAsOf is FinishPlay for a report made at [at] and sent later; like
// SetProgressAsOf it applies only if nothing newer was recorded meanwhile.
func (s *Store) FinishPlayAsOf(ctx context.Context, userID, guid string, at time.Time) (bool, error) {
	return s.finishPlay(ctx, userID, guid, asOfStamp(at))
}

func (s *Store) finishPlay(ctx context.Context, userID, guid, asOf string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, play_count, played, last_played_at)
		VALUES (?, ?, 0, 1, 1, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET
			play_count = user_item_data.play_count + CASE WHEN user_item_data.played = 1 AND user_item_data.position_ms = 0 THEN 0 ELSE 1 END,
			position_ms = 0, played = 1, last_played_at = excluded.last_played_at`+unlessNewer,
		userID, guid, stampOr(asOf), asOf, asOf, asOf)
	return applied(res, err)
}

// MarkPlayed sets played or unplayed (resetting position). Marking an unplayed item
// watched counts a play now; an item already played keeps its play count and last
// played time, so "mark all watched" over a season half seen does not count the seen
// half again or move it to the top of history.
func (s *Store) MarkPlayed(ctx context.Context, userID, guid string, played bool) error {
	return markPlayed(ctx, s.db, userID, guid, played)
}

// MarkPlayedAsOf is MarkPlayed(played) for a mark made at [at] and sent later (a phone
// replaying an episode it finished offline): like SetProgressAsOf it applies only if
// nothing newer was recorded for the item meanwhile, so a rewatch on another device since
// is not flipped back to watched. The check is part of the same statement. False when it
// was not applied.
func (s *Store) MarkPlayedAsOf(ctx context.Context, userID, guid string, at time.Time) (bool, error) {
	asOf := asOfStamp(at)
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, play_count, played, last_played_at, marked_at)
		VALUES (?, ?, 0, 1, 1, ?, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET position_ms = 0,
			play_count = user_item_data.play_count + CASE WHEN user_item_data.played = 1 THEN 0 ELSE 1 END,
			last_played_at = CASE WHEN user_item_data.played = 1 THEN user_item_data.last_played_at ELSE excluded.last_played_at END,
			played = 1, marked_at = excluded.marked_at`+unlessNewer,
		userID, guid, asOf, asOf, asOf, asOf, asOf)
	return applied(res, err)
}

// UnmarkPlayedAsOf is MarkPlayed(false) for an unmark made at [at] and sent later: it
// applies only if nothing newer was recorded for the item meanwhile.
func (s *Store) UnmarkPlayedAsOf(ctx context.Context, userID, guid string, at time.Time) (bool, error) {
	asOf := asOfStamp(at)
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, played, marked_at) VALUES (?, ?, 0, 0, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET position_ms = 0, played = 0, marked_at = excluded.marked_at`+unlessNewer,
		userID, guid, asOf, asOf, asOf, asOf)
	return applied(res, err)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func markPlayed(ctx context.Context, db execer, userID, guid string, played bool) error {
	if played {
		now := Now()
		_, err := db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, play_count, played, last_played_at, marked_at)
			VALUES (?, ?, 0, 1, 1, ?, ?)
			ON CONFLICT(scope_id, item_guid) DO UPDATE SET position_ms = 0,
				play_count = user_item_data.play_count + CASE WHEN user_item_data.played = 1 THEN 0 ELSE 1 END,
				last_played_at = CASE WHEN user_item_data.played = 1 THEN user_item_data.last_played_at ELSE excluded.last_played_at END,
				played = 1, marked_at = excluded.marked_at`,
			userID, guid, now, now)
		return err
	}
	// The time of the unmark is kept (marked_at): an offline report older than it does
	// not mark the item watched again.
	_, err := db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, position_ms, played, marked_at) VALUES (?, ?, 0, 0, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET position_ms = 0, played = 0, marked_at = excluded.marked_at`, userID, guid, Now())
	return err
}

// MarkPlayedUnder flags every episode beneath a series or season, so "mark all as
// watched" on a title page is one call. The parent row itself carries no flag: a
// series' progress is derived from its episodes. Marking watched skips placeholders
// (missing or unaired episodes): they share the GUID of the file that arrives later,
// which would otherwise show up already watched. Marking unwatched clears them too.
// One transaction: a failure part way leaves the season as it was, not half marked.
func (s *Store) MarkPlayedUnder(ctx context.Context, userID, itemID string, played bool) error {
	q := `WITH RECURSIVE tree(id) AS (
			SELECT id FROM items WHERE id = ?
			UNION ALL
			SELECT i.id FROM items i JOIN tree t ON i.parent_id = t.id
		)
		SELECT guid FROM items WHERE type = 'episode' AND id IN (SELECT id FROM tree)`
	if played {
		q += ` AND is_virtual = 0`
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, q, itemID)
	if err != nil {
		return err
	}
	var guids []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			rows.Close()
			return err
		}
		guids = append(guids, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, g := range guids {
		if err := markPlayed(ctx, tx, userID, g, played); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetFavorite(ctx context.Context, userID, guid string, fav bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (scope_id, item_guid, is_favorite) VALUES (?, ?, ?)
		ON CONFLICT(scope_id, item_guid) DO UPDATE SET is_favorite = excluded.is_favorite`, userID, guid, boolInt(fav))
	return err
}

// ResumeItems lists in-progress items (position > 0), most recent first. A played item
// with a position is a rewatch under way (finishing resets the position to 0), so it
// counts as in progress too.
func (s *Store) ResumeItems(ctx context.Context, userID string, limit int) ([]model.Item, error) {
	if limit <= 0 {
		limit = 20
	}
	// One entry per show: for episodes, keep only the most recently played episode of
	// each series (grouped by the season's parent); movies stand on their own.
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM items i
		JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE u.position_ms > 0 AND i.type IN ('movie', 'episode')
		  AND NOT EXISTS (
		    SELECT 1 FROM items i2
		    JOIN user_item_data u2 ON u2.item_guid = i2.guid AND u2.scope_id = u.scope_id
		    JOIN items s1 ON s1.id = i.parent_id
		    JOIN items s2 ON s2.id = i2.parent_id
		    WHERE i.type = 'episode' AND i2.type = 'episode' AND i2.id <> i.id
		      AND s1.parent_id = s2.parent_id
		      AND u2.position_ms > 0
		      AND (u2.last_played_at > u.last_played_at OR (u2.last_played_at = u.last_played_at AND i2.id > i.id))
		  )
		ORDER BY u.last_played_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.fillItemExtras(ctx, out)
}

// HistoryItems lists the episodes and movies the profile has played (started or
// finished), most recently played first: My Lists' History tab.
func (s *Store) HistoryItems(ctx context.Context, userID string, limit int) ([]model.Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM items i
		JOIN user_item_data u ON u.item_guid = i.guid AND u.scope_id = ?
		WHERE (u.position_ms > 0 OR u.played = 1) AND u.last_played_at IS NOT NULL AND u.last_played_at <> ''
		  AND i.type IN ('movie', 'episode')
		ORDER BY u.last_played_at DESC, i.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.fillItemExtras(ctx, out)
}

// UserDataForItems fetches watch state for many items (by GUID) in one query.
func (s *Store) UserDataForItems(ctx context.Context, userID string, guids []string) (map[string]*UserItemData, error) {
	out := map[string]*UserItemData{}
	if len(guids) == 0 {
		return out, nil
	}
	const chunk = 500
	for start := 0; start < len(guids); start += chunk {
		end := start + chunk
		if end > len(guids) {
			end = len(guids)
		}
		part := guids[start:end]
		ph := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, 0, len(part)+1)
		args = append(args, userID)
		for _, g := range part {
			args = append(args, g)
		}
		rows, err := s.db.QueryContext(ctx, `SELECT scope_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index
			FROM user_item_data WHERE scope_id = ? AND item_guid IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d UserItemData
			var played, fav int
			var last sql.NullString
			var audio, sub sql.NullInt64
			if err := rows.Scan(&d.UserID, &d.ItemGUID, &d.PositionMs, &d.PlayCount, &played, &fav, &last, &audio, &sub); err != nil {
				rows.Close()
				return nil, err
			}
			d.Played, d.IsFavorite = played == 1, fav == 1
			d.LastPlayedAt = ParseTime(last.String)
			d.AudioStreamIndex, d.SubtitleStreamIndex = ptrInt(audio), ptrInt(sub)
			dd := d
			out[d.ItemGUID] = &dd
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// Native renders the watch state as the native API's userData object (never nil).
func (d *UserItemData) Native() *model.UserData {
	if d == nil {
		return &model.UserData{}
	}
	u := &model.UserData{PositionMs: d.PositionMs, Played: d.Played, PlayCount: d.PlayCount, IsFavorite: d.IsFavorite,
		AudioIndex: d.AudioStreamIndex, SubtitleIndex: d.SubtitleStreamIndex}
	if !d.LastPlayedAt.IsZero() {
		t := d.LastPlayedAt
		u.LastPlayedAt = &t
	}
	return u
}
