package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Playlists: a profile's lists of songs. Every function takes the profile (scope) and
// touches only that profile's lists: a list id from another profile is "not found".

// MaxPlaylistName is the longest name a playlist can have.
const MaxPlaylistName = 120

// MaxPlaylistEntries is the most entries one playlist holds.
const MaxPlaylistEntries = 10000

// ErrPlaylistFull: adding these songs would take a playlist past MaxPlaylistEntries.
var ErrPlaylistFull = errors.New("the playlist is full")

const playlistCols = `p.id, p.name, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM music_playlist_tracks e JOIN music_tracks t ON t.id = e.track_id WHERE e.playlist_id = p.id AND t.missing_since IS NULL),
	(SELECT COALESCE(SUM(t.duration_ms), 0) FROM music_playlist_tracks e JOIN music_tracks t ON t.id = e.track_id WHERE e.playlist_id = p.id AND t.missing_since IS NULL)`

func scanPlaylist(row interface{ Scan(...any) error }) (model.MusicPlaylist, error) {
	var p model.MusicPlaylist
	var created, updated string
	err := row.Scan(&p.ID, &p.Name, &created, &updated, &p.TrackCount, &p.DurationMs)
	p.CreatedAt, p.UpdatedAt = ParseTime(created), ParseTime(updated)
	p.Art = []string{}
	return p, err
}

// playlistArt fills in each playlist's cover pictures: those of its first songs, one of
// each, four at most.
func (s *Store) playlistArt(ctx context.Context, lists []model.MusicPlaylist) error {
	for i := range lists {
		rows, err := s.db.QueryContext(ctx, `SELECT art FROM (
				SELECT CASE WHEN t.art <> '' THEN t.art ELSE COALESCE(al.art, '') END AS art, MIN(e.position) AS first
				FROM music_playlist_tracks e JOIN music_tracks t ON t.id = e.track_id LEFT JOIN music_albums al ON al.id = t.album_id
				WHERE e.playlist_id = ? AND t.missing_since IS NULL GROUP BY 1)
			WHERE art <> '' ORDER BY first LIMIT 4`, lists[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return err
			}
			lists[i].Art = append(lists[i].Art, h)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

// ListMusicPlaylists lists a profile's playlists, the one changed last first.
func (s *Store) ListMusicPlaylists(ctx context.Context, scope string) ([]model.MusicPlaylist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+playlistCols+` FROM music_playlists p WHERE p.scope_id = ? ORDER BY p.updated_at DESC, p.id`, scope)
	if err != nil {
		return nil, err
	}
	out := []model.MusicPlaylist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, s.playlistArt(ctx, out)
}

// GetMusicPlaylist is one of a profile's playlists (nil when it has none with this id).
func (s *Store) GetMusicPlaylist(ctx context.Context, scope, id string) (*model.MusicPlaylist, error) {
	p, err := scanPlaylist(s.db.QueryRowContext(ctx, `SELECT `+playlistCols+` FROM music_playlists p WHERE p.id = ? AND p.scope_id = ?`, id, scope))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	one := []model.MusicPlaylist{p}
	if err := s.playlistArt(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// CleanPlaylistName trims a name and cuts it to MaxPlaylistName; "" when nothing is left.
func CleanPlaylistName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > MaxPlaylistName {
		name = strings.TrimSpace(string(r[:MaxPlaylistName]))
	}
	return name
}

// CreateMusicPlaylist makes a playlist for a profile, holding trackIDs in that order
// (ids that are not songs are left out).
func (s *Store) CreateMusicPlaylist(ctx context.Context, scope, name string, trackIDs []string) (*model.MusicPlaylist, error) {
	id := uuid.NewString()
	now := Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_playlists (id, scope_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, scope, name, now, now); err != nil {
		return nil, err
	}
	if _, err := appendPlaylistTracks(ctx, tx, id, trackIDs, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMusicPlaylist(ctx, scope, id)
}

// appendPlaylistTracks adds songs at the end of a playlist and says how many went in.
func appendPlaylistTracks(ctx context.Context, tx *sql.Tx, playlistID string, trackIDs []string, now string) (int, error) {
	if len(trackIDs) == 0 {
		return 0, nil
	}
	var next, have int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) + 1, COUNT(*) FROM music_playlist_tracks WHERE playlist_id = ?`, playlistID).Scan(&next, &have); err != nil {
		return 0, err
	}
	added := 0
	for _, trackID := range trackIDs {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_tracks WHERE id = ?`, trackID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			continue
		}
		if have+added >= MaxPlaylistEntries {
			return 0, ErrPlaylistFull
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)`, playlistID, trackID, next, now); err != nil {
			return 0, err
		}
		next++
		added++
	}
	return added, nil
}

// ownPlaylist begins a change to a profile's playlist: a transaction, once the playlist
// is known to be that profile's (ok is false when it is not).
func (s *Store) ownPlaylist(ctx context.Context, scope, id string) (tx *sql.Tx, ok bool, err error) {
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_playlists WHERE id = ? AND scope_id = ?`, id, scope).Scan(&n); err != nil {
		tx.Rollback()
		return nil, false, err
	}
	if n == 0 {
		tx.Rollback()
		return nil, false, nil
	}
	return tx, true, nil
}

func touchPlaylist(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `UPDATE music_playlists SET updated_at = ? WHERE id = ?`, Now(), id)
	return err
}

// RenameMusicPlaylist renames a profile's playlist; false when it has none with this id.
func (s *Store) RenameMusicPlaylist(ctx context.Context, scope, id, name string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE music_playlists SET name = ?, updated_at = ? WHERE id = ? AND scope_id = ?`, name, Now(), id, scope)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteMusicPlaylist removes a profile's playlist; false when it has none with this id.
func (s *Store) DeleteMusicPlaylist(ctx context.Context, scope, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM music_playlists WHERE id = ? AND scope_id = ?`, id, scope)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AddToMusicPlaylist adds songs at the end of a profile's playlist and says how many
// went in (ids that are not songs are left out). ok is false when the profile has no
// playlist with this id.
func (s *Store) AddToMusicPlaylist(ctx context.Context, scope, id string, trackIDs []string) (added int, ok bool, err error) {
	tx, ok, err := s.ownPlaylist(ctx, scope, id)
	if err != nil || !ok {
		return 0, ok, err
	}
	defer tx.Rollback()
	if added, err = appendPlaylistTracks(ctx, tx, id, trackIDs, Now()); err != nil {
		return 0, true, err
	}
	if added > 0 {
		if err := touchPlaylist(ctx, tx, id); err != nil {
			return 0, true, err
		}
	}
	return added, true, tx.Commit()
}

// RemoveFromMusicPlaylist takes entries out of a profile's playlist (entries that are
// not in it are ignored) and closes the gaps they leave.
func (s *Store) RemoveFromMusicPlaylist(ctx context.Context, scope, id string, entryIDs []int64) (ok bool, err error) {
	tx, ok, err := s.ownPlaylist(ctx, scope, id)
	if err != nil || !ok {
		return ok, err
	}
	defer tx.Rollback()
	for _, e := range entryIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_playlist_tracks WHERE id = ? AND playlist_id = ?`, e, id); err != nil {
			return true, err
		}
	}
	if err := renumberPlaylist(ctx, tx, id, nil); err != nil {
		return true, err
	}
	if err := touchPlaylist(ctx, tx, id); err != nil {
		return true, err
	}
	return true, tx.Commit()
}

// OrderMusicPlaylist puts a profile's playlist in the order given (entry ids). Entries
// the order does not name keep their order after the ones it does; ids that are not the
// playlist's are ignored. So a client that orders the songs it shows cannot lose the
// ones it does not (a song whose file is gone for now).
func (s *Store) OrderMusicPlaylist(ctx context.Context, scope, id string, entryIDs []int64) (ok bool, err error) {
	tx, ok, err := s.ownPlaylist(ctx, scope, id)
	if err != nil || !ok {
		return ok, err
	}
	defer tx.Rollback()
	if err := renumberPlaylist(ctx, tx, id, entryIDs); err != nil {
		return true, err
	}
	if err := touchPlaylist(ctx, tx, id); err != nil {
		return true, err
	}
	return true, tx.Commit()
}

// renumberPlaylist gives a playlist's entries the positions 0, 1, 2 ...: first the ones
// `first` names, in that order, then the rest in the order they had.
func renumberPlaylist(ctx context.Context, tx *sql.Tx, id string, first []int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM music_playlist_tracks WHERE playlist_id = ? ORDER BY position, id`, id)
	if err != nil {
		return err
	}
	var have []int64
	in := map[int64]bool{}
	for rows.Next() {
		var e int64
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return err
		}
		have = append(have, e)
		in[e] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	order := make([]int64, 0, len(have))
	placed := map[int64]bool{}
	for _, e := range first {
		if in[e] && !placed[e] {
			placed[e] = true
			order = append(order, e)
		}
	}
	for _, e := range have {
		if !placed[e] {
			order = append(order, e)
		}
	}
	for pos, e := range order {
		if _, err := tx.ExecContext(ctx, `UPDATE music_playlist_tracks SET position = ? WHERE id = ?`, pos, e); err != nil {
			return err
		}
	}
	return nil
}

// MusicPlaylistEntries lists a profile's playlist in its order: the songs whose files
// are there, each with its entry id. Nil when the profile has no playlist with this id.
func (s *Store) MusicPlaylistEntries(ctx context.Context, scope, id string) ([]model.PlaylistEntry, error) {
	p, err := s.GetMusicPlaylist(ctx, scope, id)
	if err != nil || p == nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, `+musicTrackCols+musicTrackFrom+`
		JOIN music_playlist_tracks e ON e.track_id = t.id
		WHERE e.playlist_id = ? AND t.missing_since IS NULL ORDER BY e.position, e.id`, scope, id)
	if err != nil {
		return nil, err
	}
	out := []model.PlaylistEntry{}
	var tracks []model.MusicTrack
	for rows.Next() {
		var entry int64
		t, err := scanMusicTrack(prefixed{rows: rows, first: &entry})
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, model.PlaylistEntry{EntryID: entry})
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// A song that is in the list twice is filled in once per place.
	if err := s.fillMusicCreditsEach(ctx, tracks); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].MusicTrack = tracks[i]
	}
	return out, nil
}

// prefixed scans one more column in front of the ones a row scanner asks for.
type prefixed struct {
	rows  *sql.Rows
	first any
}

func (p prefixed) Scan(dest ...any) error {
	return p.rows.Scan(append([]any{p.first}, dest...)...)
}

// fillMusicCreditsEach is fillMusicCredits for a list in which a track may appear more
// than once.
func (s *Store) fillMusicCreditsEach(ctx context.Context, tracks []model.MusicTrack) error {
	seen := map[string]int{}
	var unique []model.MusicTrack
	for _, t := range tracks {
		if _, ok := seen[t.ID]; !ok {
			seen[t.ID] = len(unique)
			unique = append(unique, t)
		}
	}
	if err := s.fillMusicCredits(ctx, unique); err != nil {
		return err
	}
	for i := range tracks {
		tracks[i].Artists = unique[seen[tracks[i].ID]].Artists
	}
	return nil
}

// MovePlaylistEntries points every playlist entry of one track at another (the first
// track's row is about to go: another file of the same song stays).
func movePlaylistEntries(ctx context.Context, tx *sql.Tx, fromID, toID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE music_playlist_tracks SET track_id = ? WHERE track_id = ?`, toID, fromID)
	return err
}
