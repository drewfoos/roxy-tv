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
	err := s.db.QueryRowContext(ctx, `SELECT user_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index
		FROM user_item_data WHERE user_id = ? AND item_guid = ?`, userID, guid).
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

// SetProgress records a playback position (and track selections) for a user/item.
func (s *Store) SetProgress(ctx context.Context, userID, guid string, positionMs int64, audio, sub *int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (user_id, item_guid, position_ms, last_played_at, audio_stream_index, subtitle_stream_index)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, item_guid) DO UPDATE SET position_ms = excluded.position_ms, last_played_at = excluded.last_played_at,
			audio_stream_index = COALESCE(excluded.audio_stream_index, user_item_data.audio_stream_index),
			subtitle_stream_index = COALESCE(excluded.subtitle_stream_index, user_item_data.subtitle_stream_index)`,
		userID, guid, positionMs, Now(), intPtrNull(audio), intPtrNull(sub))
	return err
}

// MarkPlayed sets played (incrementing play count) or unplayed (resetting position).
func (s *Store) MarkPlayed(ctx context.Context, userID, guid string, played bool) error {
	if played {
		_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (user_id, item_guid, position_ms, play_count, played, last_played_at)
			VALUES (?, ?, 0, 1, 1, ?)
			ON CONFLICT(user_id, item_guid) DO UPDATE SET position_ms = 0, play_count = user_item_data.play_count + 1, played = 1, last_played_at = excluded.last_played_at`,
			userID, guid, Now())
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (user_id, item_guid, position_ms, played) VALUES (?, ?, 0, 0)
		ON CONFLICT(user_id, item_guid) DO UPDATE SET position_ms = 0, played = 0`, userID, guid)
	return err
}

func (s *Store) SetFavorite(ctx context.Context, userID, guid string, fav bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_data (user_id, item_guid, is_favorite) VALUES (?, ?, ?)
		ON CONFLICT(user_id, item_guid) DO UPDATE SET is_favorite = excluded.is_favorite`, userID, guid, boolInt(fav))
	return err
}

// ResumeItems lists in-progress items (position > 0, not played), most recent first.
func (s *Store) ResumeItems(ctx context.Context, userID string, limit int) ([]model.Item, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM items i
		JOIN user_item_data u ON u.item_guid = i.guid AND u.user_id = ?
		WHERE u.position_ms > 0 AND u.played = 0 AND i.type IN ('movie', 'episode')
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
		rows, err := s.db.QueryContext(ctx, `SELECT user_id, item_guid, position_ms, play_count, played, is_favorite, last_played_at, audio_stream_index, subtitle_stream_index
			FROM user_item_data WHERE user_id = ? AND item_guid IN (`+ph+`)`, args...)
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
