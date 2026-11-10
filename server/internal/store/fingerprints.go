package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Fingerprint is a stored Chromaprint fingerprint of part of a source's audio.
type Fingerprint struct {
	SourceID string
	Kind     string // "head" | "tail"
	StartMs  int64
	LengthMs int64
	Points   []uint32
}

func (s *Store) GetFingerprint(ctx context.Context, sourceID, kind string) (*Fingerprint, error) {
	var fp Fingerprint
	var blob []byte
	err := s.db.QueryRowContext(ctx, `SELECT source_id, kind, start_ms, length_ms, points FROM audio_fingerprints WHERE source_id = ? AND kind = ?`, sourceID, kind).
		Scan(&fp.SourceID, &fp.Kind, &fp.StartMs, &fp.LengthMs, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fp.Points = decodePoints(blob)
	return &fp, nil
}

func (s *Store) PutFingerprint(ctx context.Context, fp *Fingerprint) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audio_fingerprints (source_id, kind, start_ms, length_ms, points, created_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(source_id, kind) DO UPDATE SET start_ms = excluded.start_ms, length_ms = excluded.length_ms, points = excluded.points, created_at = excluded.created_at`,
		fp.SourceID, fp.Kind, fp.StartMs, fp.LengthMs, encodePoints(fp.Points), Now())
	return err
}

func encodePoints(p []uint32) []byte {
	b := make([]byte, 4*len(p))
	for i, v := range p {
		binary.LittleEndian.PutUint32(b[4*i:], v)
	}
	return b
}

func decodePoints(b []byte) []uint32 {
	p := make([]uint32, len(b)/4)
	for i := range p {
		p[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return p
}

// SeasonEpisode is what the segment detector needs per episode of a season.
type SeasonEpisode struct {
	ItemID     string
	SourceID   string
	Path       string
	Index      int
	DurationMs int64
	Segments   []model.MediaSegment
	// DetectVersion is the detector version that last worked on the source (0 = never).
	DetectVersion int
	// AniSkipMal is the MAL id AniSkip was last asked with for the source; AniSkipAsked
	// is false when it never was.
	AniSkipMal   string
	AniSkipAsked bool
	// FailedPasses is how many detection passes in a row could not fingerprint the
	// source's audio (NoteFingerprintFailure).
	FailedPasses int
}

// SeasonEpisodes lists a season's episodes that have a primary file, in aired order,
// with the segments known so far.
func (s *Store) SeasonEpisodes(ctx context.Context, seasonID string) ([]SeasonEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, COALESCE(i.index_aired, 0), ms.id, ms.duration_ms, mp.path, COALESCE(sd.version, 0), sd.aniskip_mal, COALESCE(sd.failed_passes, 0)
		FROM items i
		JOIN media_sources ms ON ms.item_id = i.id AND ms.is_primary = 1
		JOIN media_parts mp ON mp.source_id = ms.id AND mp.part_index = 0
		LEFT JOIN segment_detection sd ON sd.source_id = ms.id
		WHERE i.parent_id = ? AND i.type = 'episode'
		ORDER BY COALESCE(i.index_aired, 0), i.id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeasonEpisode
	for rows.Next() {
		var e SeasonEpisode
		var mal sql.NullString
		if err := rows.Scan(&e.ItemID, &e.Index, &e.SourceID, &e.DurationMs, &e.Path, &e.DetectVersion, &mal, &e.FailedPasses); err != nil {
			return nil, err
		}
		e.AniSkipMal, e.AniSkipAsked = mal.String, mal.Valid
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		segs, err := s.db.QueryContext(ctx, `SELECT type, start_ms, end_ms, origin FROM media_segments WHERE source_id = ? ORDER BY start_ms`, out[i].SourceID)
		if err != nil {
			return nil, err
		}
		for segs.Next() {
			var sg model.MediaSegment
			if err := segs.Scan(&sg.Type, &sg.StartMs, &sg.EndMs, &sg.Origin); err != nil {
				segs.Close()
				return nil, err
			}
			out[i].Segments = append(out[i].Segments, sg)
		}
		segs.Close()
	}
	return out, nil
}

// SeasonIDs lists every season id (for sweeps).
func (s *Store) SeasonIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM items WHERE type = 'season' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SeasonOfSource returns the season (parent) id of the episode a source belongs to, or "".
func (s *Store) SeasonOfSource(ctx context.Context, sourceID string) (string, error) {
	var parent sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT i.parent_id FROM media_sources ms JOIN items i ON i.id = ms.item_id WHERE ms.id = ? AND i.type = 'episode'`, sourceID).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return parent.String, err
}

// DeleteSegmentTypes removes a source's segments of one origin and the given types.
func (s *Store) DeleteSegmentTypes(ctx context.Context, sourceID, origin string, types []string) error {
	for _, t := range types {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media_segments WHERE source_id = ? AND origin = ? AND type = ?`, sourceID, origin, t); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceSegmentsByOrigin swaps a source's segments of one origin for the given set.
// Segments of other origins (chapters, manual) are left alone; a conflict on type keeps
// the existing row (chapters beat fingerprints).
func (s *Store) ReplaceSegmentsByOrigin(ctx context.Context, sourceID, origin string, segs []model.MediaSegment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_segments WHERE source_id = ? AND origin = ?`, sourceID, origin); err != nil {
		return err
	}
	for _, sg := range segs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_segments (source_id, type, start_ms, end_ms, origin) VALUES (?,?,?,?,?) ON CONFLICT(source_id, type) DO NOTHING`,
			sourceID, sg.Type, sg.StartMs, sg.EndMs, origin); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkDetected records that the segment detector (at version) worked on a source.
func (s *Store) MarkDetected(ctx context.Context, sourceID string, version int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO segment_detection (source_id, version, detected_at) VALUES (?,?,?)
		ON CONFLICT(source_id) DO UPDATE SET version = excluded.version, detected_at = excluded.detected_at`, sourceID, version, Now())
	return err
}

// NoteFingerprintFailure counts a detection pass that could not fingerprint the source's
// audio. The source stays "never worked on" (version 0), so the next sweep tries again.
func (s *Store) NoteFingerprintFailure(ctx context.Context, sourceID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO segment_detection (source_id, version, detected_at, failed_passes) VALUES (?,0,?,1)
		ON CONFLICT(source_id) DO UPDATE SET failed_passes = failed_passes + 1`, sourceID, Now())
	return err
}

// MarkAniSkipAsked records that AniSkip was asked about a source with mal ("" = the
// season had no MAL id, so there was nothing to ask).
func (s *Store) MarkAniSkipAsked(ctx context.Context, sourceID, mal string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO segment_detection (source_id, version, detected_at, aniskip_mal) VALUES (?,0,?,?)
		ON CONFLICT(source_id) DO UPDATE SET aniskip_mal = excluded.aniskip_mal`, sourceID, Now(), mal)
	return err
}
