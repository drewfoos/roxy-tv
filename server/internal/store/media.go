package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ReplaceSource writes a probed media source with its parts, streams and attachments in
// one transaction, replacing any previous rows for the same source id.
func (s *Store) ReplaceSource(ctx context.Context, src *model.MediaSource, probeJSON string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_sources (id, item_id, name, container, size, duration_ms, bitrate, version_label, is_primary, probed_at, probe_json)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET item_id=excluded.item_id, name=excluded.name, container=excluded.container, size=excluded.size,
		duration_ms=excluded.duration_ms, bitrate=excluded.bitrate, version_label=excluded.version_label, is_primary=excluded.is_primary,
		probed_at=excluded.probed_at, probe_json=excluded.probe_json`,
		src.ID, src.ItemID, nullStr(src.Name), nullStr(src.Container), src.Size, src.DurationMs, nullInt64(src.Bitrate), nullStr(src.VersionLabel), boolInt(src.IsPrimary), now, nullStr(probeJSON)); err != nil {
		return err
	}
	for _, t := range []string{"media_parts", "media_streams", "attachments", "media_segments"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE source_id = ?`, src.ID); err != nil {
			return err
		}
	}
	for _, p := range src.Parts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_parts (id, source_id, part_index, path, size, mtime, duration_ms) VALUES (?,?,?,?,?,?,?)`,
			p.ID, src.ID, p.Index, p.Path, p.Size, p.ModTime.UTC().Format(TimeFormat), nullInt64(p.DurationMs)); err != nil {
			return err
		}
	}
	for _, st := range src.Streams {
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_streams (source_id, stream_index, type, codec, profile, level, bit_depth, pix_fmt, width, height, fps,
			color_primaries, color_transfer, color_space, range_type, dv_profile, channels, channel_layout, sample_rate, bitrate, language, title,
			is_default, is_forced, is_hearing_impaired, is_external, external_path, is_text_format, detected_language)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			src.ID, st.Index, st.Type, st.Codec, nullStr(st.Profile), nullInt(st.Level), nullInt(st.BitDepth), nullStr(st.PixFmt), nullInt(st.Width), nullInt(st.Height), nullFloat(st.FPS),
			nullStr(st.ColorPrimaries), nullStr(st.ColorTransfer), nullStr(st.ColorSpace), nullStr(string(st.RangeType)), nullInt(st.DVProfile), nullInt(st.Channels), nullStr(st.ChannelLayout), nullInt(st.SampleRate), nullInt64(st.Bitrate), nullStr(st.Language), nullStr(st.Title),
			boolInt(st.IsDefault), boolInt(st.IsForced), boolInt(st.IsHearingImpaired), boolInt(st.IsExternal), nullStr(st.ExternalPath), boolInt(st.IsTextFormat), nullStr(st.DetectedLanguage)); err != nil {
			return err
		}
	}
	for _, a := range src.Attachments {
		var at sql.NullString
		if a.ExtractedAt != nil {
			at = nullStr(a.ExtractedAt.UTC().Format(TimeFormat))
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO attachments (source_id, stream_index, filename, mime, extracted_path, extracted_at) VALUES (?,?,?,?,?,?)`,
			src.ID, a.Index, a.Filename, nullStr(a.Mime), nullStr(a.ExtractedPath), at); err != nil {
			return err
		}
	}
	for _, sg := range src.Segments {
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_segments (source_id, type, start_ms, end_ms, origin) VALUES (?,?,?,?,?)
			ON CONFLICT(source_id, type) DO UPDATE SET start_ms = excluded.start_ms, end_ms = excluded.end_ms, origin = excluded.origin`,
			src.ID, sg.Type, sg.StartMs, sg.EndMs, sg.Origin); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetSource(ctx context.Context, id string) (*model.MediaSource, error) {
	var src model.MediaSource
	var name, container, label sql.NullString
	var bitrate sql.NullInt64
	var primary int
	err := s.db.QueryRowContext(ctx, `SELECT id, item_id, name, container, size, duration_ms, bitrate, version_label, is_primary FROM media_sources WHERE id = ?`, id).
		Scan(&src.ID, &src.ItemID, &name, &container, &src.Size, &src.DurationMs, &bitrate, &label, &primary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	src.Name, src.Container, src.VersionLabel, src.Bitrate, src.IsPrimary = name.String, container.String, label.String, bitrate.Int64, primary == 1
	if err := s.loadSourceChildren(ctx, &src); err != nil {
		return nil, err
	}
	return &src, nil
}

func (s *Store) SourcesForItem(ctx context.Context, itemID string) ([]model.MediaSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM media_sources WHERE item_id = ? ORDER BY is_primary DESC, name`, itemID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []model.MediaSource
	for _, id := range ids {
		src, err := s.GetSource(ctx, id)
		if err != nil {
			return nil, err
		}
		if src != nil {
			out = append(out, *src)
		}
	}
	return out, nil
}

// FindSourceByPathStem returns the source whose first part's file name (without
// extension) equals stem. Used by the /stream/{name} convenience route.
func (s *Store) FindSourceByPathStem(ctx context.Context, stem string) (*model.MediaSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, path FROM media_parts WHERE part_index = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		base := filepath.Base(p)
		if strings.EqualFold(strings.TrimSuffix(base, filepath.Ext(base)), stem) {
			rows.Close()
			return s.GetSource(ctx, id)
		}
	}
	return nil, rows.Err()
}

func (s *Store) GetPartByPath(ctx context.Context, path string) (*model.MediaPart, error) {
	var p model.MediaPart
	var mtime string
	var dur sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, source_id, part_index, path, size, mtime, duration_ms FROM media_parts WHERE path = ?`, path).
		Scan(&p.ID, &p.SourceID, &p.Index, &p.Path, &p.Size, &mtime, &dur)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.ModTime = ParseTime(mtime)
	p.DurationMs = dur.Int64
	return &p, nil
}

func (s *Store) ListPartPaths(ctx context.Context, libraryID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mp.path, mp.source_id FROM media_parts mp
		JOIN media_sources ms ON ms.id = mp.source_id JOIN items i ON i.id = ms.item_id WHERE i.library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var p, id string
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		out[p] = id
	}
	return out, rows.Err()
}

// ItemPath is a media file and the item (movie or episode) it belongs to.
type ItemPath struct {
	ItemID string
	Path   string
}

// PathsUnder lists the media files of an item: a movie's own, or every episode's under a
// series (for reading .nfo files and artwork next to them).
func (s *Store) PathsUnder(ctx context.Context, itemID string) ([]ItemPath, error) {
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE tree(id) AS (
			SELECT id FROM items WHERE id = ?
			UNION ALL
			SELECT i.id FROM items i JOIN tree t ON i.parent_id = t.id
		)
		SELECT ms.item_id, mp.path FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
		WHERE ms.item_id IN (SELECT id FROM tree) ORDER BY mp.path`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemPath
	for rows.Next() {
		var p ItemPath
		if err := rows.Scan(&p.ItemID, &p.Path); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AllSourceIDs lists every media source id, newest items first.
func (s *Store) AllSourceIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ms.id FROM media_sources ms JOIN items i ON i.id = ms.item_id ORDER BY i.date_created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSource(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_sources WHERE id = ?`, id)
	return err
}

func (s *Store) loadSourceChildren(ctx context.Context, src *model.MediaSource) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, part_index, path, size, mtime, duration_ms FROM media_parts WHERE source_id = ? ORDER BY part_index`, src.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p model.MediaPart
		var mtime string
		var dur sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Index, &p.Path, &p.Size, &mtime, &dur); err != nil {
			rows.Close()
			return err
		}
		p.SourceID = src.ID
		p.ModTime = ParseTime(mtime)
		p.DurationMs = dur.Int64
		src.Parts = append(src.Parts, p)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT stream_index, type, codec, profile, level, bit_depth, pix_fmt, width, height, fps,
		color_primaries, color_transfer, color_space, range_type, dv_profile, channels, channel_layout, sample_rate, bitrate, language, title,
		is_default, is_forced, is_hearing_impaired, is_external, external_path, is_text_format, detected_language FROM media_streams WHERE source_id = ? ORDER BY stream_index`, src.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var st model.MediaStream
		var profile, pixFmt, prim, trc, space, rng, layout, lang, title, extPath, detected sql.NullString
		var level, depth, w, h, dv, ch, sr sql.NullInt64
		var fps sql.NullFloat64
		var br sql.NullInt64
		var def, forced, hi, ext, text int
		if err := rows.Scan(&st.Index, &st.Type, &st.Codec, &profile, &level, &depth, &pixFmt, &w, &h, &fps,
			&prim, &trc, &space, &rng, &dv, &ch, &layout, &sr, &br, &lang, &title, &def, &forced, &hi, &ext, &extPath, &text, &detected); err != nil {
			rows.Close()
			return err
		}
		st.SourceID = src.ID
		st.Profile, st.PixFmt, st.ColorPrimaries, st.ColorTransfer, st.ColorSpace = profile.String, pixFmt.String, prim.String, trc.String, space.String
		st.RangeType = model.RangeType(rng.String)
		st.ChannelLayout, st.Language, st.Title, st.ExternalPath = layout.String, lang.String, title.String, extPath.String
		st.DetectedLanguage = detected.String
		st.Level, st.BitDepth, st.Width, st.Height, st.DVProfile, st.Channels, st.SampleRate = int(level.Int64), int(depth.Int64), int(w.Int64), int(h.Int64), int(dv.Int64), int(ch.Int64), int(sr.Int64)
		st.FPS, st.Bitrate = fps.Float64, br.Int64
		st.IsDefault, st.IsForced, st.IsHearingImpaired, st.IsExternal, st.IsTextFormat = def == 1, forced == 1, hi == 1, ext == 1, text == 1
		src.Streams = append(src.Streams, st)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT stream_index, filename, mime, extracted_path, extracted_at FROM attachments WHERE source_id = ? ORDER BY stream_index`, src.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.Attachment
		var mime, path, at sql.NullString
		if err := rows.Scan(&a.Index, &a.Filename, &mime, &path, &at); err != nil {
			return err
		}
		a.SourceID = src.ID
		a.Mime, a.ExtractedPath = mime.String, path.String
		if at.Valid {
			t := ParseTime(at.String)
			if !t.IsZero() {
				tt := t
				a.ExtractedAt = &tt
			}
		}
		src.Attachments = append(src.Attachments, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT type, start_ms, end_ms, origin FROM media_segments WHERE source_id = ? ORDER BY start_ms`, src.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sg model.MediaSegment
		if err := rows.Scan(&sg.Type, &sg.StartMs, &sg.EndMs, &sg.Origin); err != nil {
			return err
		}
		src.Segments = append(src.Segments, sg)
	}
	_ = time.Now
	return rows.Err()
}
