package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

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
	// Raise the sidecar high-water mark past every sidecar number this ingest gives out
	// (it never goes down: see SidecarNextIndex).
	next := 0
	for _, st := range src.Streams {
		if st.IsExternal && st.Index+1 > next {
			next = st.Index + 1
		}
	}
	if next > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE media_sources SET sidecar_next_index = MAX(sidecar_next_index, ?) WHERE id = ?`, next, src.ID); err != nil {
			return err
		}
	}
	for _, t := range []string{"media_parts", "media_streams", "attachments", "media_segments", "segment_detection"} {
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
	srcs, err := s.querySources(ctx, `SELECT `+sourceCols+` FROM media_sources WHERE id = ?`, id)
	if err != nil || len(srcs) == 0 {
		return nil, err
	}
	return &srcs[0], nil
}

// SourcesForItem is the item's media sources, primary first.
func (s *Store) SourcesForItem(ctx context.Context, itemID string) ([]model.MediaSource, error) {
	m, err := s.SourcesForItems(ctx, []string{itemID})
	return m[itemID], err
}

// SourcesForItems is SourcesForItem for a whole page of items in a fixed number of
// queries (sources, then parts, streams, attachments and segments for all of them),
// where looping SourcesForItem cost five queries per source.
func (s *Store) SourcesForItems(ctx context.Context, itemIDs []string) (map[string][]model.MediaSource, error) {
	out := map[string][]model.MediaSource{}
	for _, ids := range chunks(itemIDs) {
		srcs, err := s.querySources(ctx, `SELECT `+sourceCols+` FROM media_sources WHERE item_id IN (`+placeholders(len(ids))+`) ORDER BY is_primary DESC, name`, anys(ids)...)
		if err != nil {
			return nil, err
		}
		for _, src := range srcs {
			out[src.ItemID] = append(out[src.ItemID], src)
		}
	}
	return out, nil
}

const sourceCols = `id, item_id, name, container, size, duration_ms, bitrate, version_label, is_primary`

// querySources reads media_sources rows (sourceCols, in order) and loads their children.
func (s *Store) querySources(ctx context.Context, query string, args ...any) ([]model.MediaSource, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var srcs []model.MediaSource
	for rows.Next() {
		var src model.MediaSource
		var name, container, label sql.NullString
		var bitrate sql.NullInt64
		var primary int
		if err := rows.Scan(&src.ID, &src.ItemID, &name, &container, &src.Size, &src.DurationMs, &bitrate, &label, &primary); err != nil {
			rows.Close()
			return nil, err
		}
		src.Name, src.Container, src.VersionLabel, src.Bitrate, src.IsPrimary = name.String, container.String, label.String, bitrate.Int64, primary == 1
		srcs = append(srcs, src)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	ptrs := make([]*model.MediaSource, len(srcs))
	for i := range srcs {
		ptrs[i] = &srcs[i]
	}
	if err := s.loadSourcesChildren(ctx, ptrs); err != nil {
		return nil, err
	}
	return srcs, nil
}

// chunks splits ids into IN-list sized groups (well under SQLite's parameter limit).
func chunks(ids []string) [][]string {
	const size = 500
	var out [][]string
	for len(ids) > size {
		out = append(out, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func anys(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
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

// ListExternalSubtitlePaths maps each source of a library to the sidecar subtitle files
// recorded for it, so a scan can tell when sidecars were added, removed or renamed beside
// a video that did not change itself.
func (s *Store) ListExternalSubtitlePaths(ctx context.Context, libraryID int64) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT st.source_id, st.external_path FROM media_streams st
		JOIN media_sources ms ON ms.id = st.source_id JOIN items i ON i.id = ms.item_id
		WHERE i.library_id = ? AND st.is_external = 1 AND st.external_path IS NOT NULL AND st.external_path <> ''`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		out[id] = append(out[id], p)
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

// SourceIDsUnder lists the media source ids of an item and everything under it.
func (s *Store) SourceIDsUnder(ctx context.Context, itemID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE tree(id) AS (
			SELECT id FROM items WHERE id = ?
			UNION ALL
			SELECT i.id FROM items i JOIN tree t ON i.parent_id = t.id
		)
		SELECT ms.id FROM media_sources ms WHERE ms.item_id IN (SELECT id FROM tree)`, itemID)
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

// SidecarNextIndex is the lowest stream number a new sidecar subtitle of the source may
// take: above every number its sidecars have ever had (0 for none yet, or no source). A
// saved subtitle choice is a stream number, so a number is never reused for another file.
func (s *Store) SidecarNextIndex(ctx context.Context, sourceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT sidecar_next_index FROM media_sources WHERE id = ?`, sourceID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

func (s *Store) DeleteSource(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_sources WHERE id = ?`, id)
	return err
}

// loadSourcesChildren fills parts, streams, attachments and segments for srcs, a fixed
// number of queries per IN-list chunk however many sources there are.
func (s *Store) loadSourcesChildren(ctx context.Context, srcs []*model.MediaSource) error {
	byID := map[string]*model.MediaSource{}
	ids := make([]string, 0, len(srcs))
	for _, src := range srcs {
		byID[src.ID] = src
		ids = append(ids, src.ID)
	}
	for _, chunk := range chunks(ids) {
		in, args := placeholders(len(chunk)), anys(chunk)
		if err := s.eachRow(ctx, `SELECT source_id, id, part_index, path, size, mtime, duration_ms FROM media_parts WHERE source_id IN (`+in+`) ORDER BY source_id, part_index`, args, func(rows *sql.Rows) error {
			var p model.MediaPart
			var mtime string
			var dur sql.NullInt64
			if err := rows.Scan(&p.SourceID, &p.ID, &p.Index, &p.Path, &p.Size, &mtime, &dur); err != nil {
				return err
			}
			p.ModTime = ParseTime(mtime)
			p.DurationMs = dur.Int64
			src := byID[p.SourceID]
			src.Parts = append(src.Parts, p)
			return nil
		}); err != nil {
			return err
		}
		if err := s.eachRow(ctx, `SELECT source_id, stream_index, type, codec, profile, level, bit_depth, pix_fmt, width, height, fps,
		color_primaries, color_transfer, color_space, range_type, dv_profile, channels, channel_layout, sample_rate, bitrate, language, title,
		is_default, is_forced, is_hearing_impaired, is_external, external_path, is_text_format, detected_language FROM media_streams WHERE source_id IN (`+in+`) ORDER BY source_id, stream_index`, args, func(rows *sql.Rows) error {
			var st model.MediaStream
			var profile, pixFmt, prim, trc, space, rng, layout, lang, title, extPath, detected sql.NullString
			var level, depth, w, h, dv, ch, sr sql.NullInt64
			var fps sql.NullFloat64
			var br sql.NullInt64
			var def, forced, hi, ext, text int
			if err := rows.Scan(&st.SourceID, &st.Index, &st.Type, &st.Codec, &profile, &level, &depth, &pixFmt, &w, &h, &fps,
				&prim, &trc, &space, &rng, &dv, &ch, &layout, &sr, &br, &lang, &title, &def, &forced, &hi, &ext, &extPath, &text, &detected); err != nil {
				return err
			}
			st.Profile, st.PixFmt, st.ColorPrimaries, st.ColorTransfer, st.ColorSpace = profile.String, pixFmt.String, prim.String, trc.String, space.String
			st.RangeType = model.RangeType(rng.String)
			st.ChannelLayout, st.Language, st.Title, st.ExternalPath = layout.String, lang.String, title.String, extPath.String
			st.DetectedLanguage = detected.String
			st.Level, st.BitDepth, st.Width, st.Height, st.DVProfile, st.Channels, st.SampleRate = int(level.Int64), int(depth.Int64), int(w.Int64), int(h.Int64), int(dv.Int64), int(ch.Int64), int(sr.Int64)
			st.FPS, st.Bitrate = fps.Float64, br.Int64
			st.IsDefault, st.IsForced, st.IsHearingImpaired, st.IsExternal, st.IsTextFormat = def == 1, forced == 1, hi == 1, ext == 1, text == 1
			src := byID[st.SourceID]
			src.Streams = append(src.Streams, st)
			return nil
		}); err != nil {
			return err
		}
		if err := s.eachRow(ctx, `SELECT source_id, stream_index, filename, mime, extracted_path, extracted_at FROM attachments WHERE source_id IN (`+in+`) ORDER BY source_id, stream_index`, args, func(rows *sql.Rows) error {
			var a model.Attachment
			var mime, path, at sql.NullString
			if err := rows.Scan(&a.SourceID, &a.Index, &a.Filename, &mime, &path, &at); err != nil {
				return err
			}
			a.Mime, a.ExtractedPath = mime.String, path.String
			if at.Valid {
				if t := ParseTime(at.String); !t.IsZero() {
					a.ExtractedAt = &t
				}
			}
			src := byID[a.SourceID]
			src.Attachments = append(src.Attachments, a)
			return nil
		}); err != nil {
			return err
		}
		if err := s.eachRow(ctx, `SELECT source_id, type, start_ms, end_ms, origin FROM media_segments WHERE source_id IN (`+in+`) ORDER BY source_id, start_ms`, args, func(rows *sql.Rows) error {
			var sg model.MediaSegment
			var sourceID string
			if err := rows.Scan(&sourceID, &sg.Type, &sg.StartMs, &sg.EndMs, &sg.Origin); err != nil {
				return err
			}
			src := byID[sourceID]
			src.Segments = append(src.Segments, sg)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// eachRow runs query and calls fn for every row, closing the rows whatever happens.
func (s *Store) eachRow(ctx context.Context, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
