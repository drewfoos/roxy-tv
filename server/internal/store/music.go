package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Music: the folders, what a scan writes, and the lists the clients read.

// ---- folders ----

func (s *Store) MusicFolders(ctx context.Context) ([]model.MusicFolder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, root, created_at FROM music_folders ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MusicFolder
	for rows.Next() {
		var f model.MusicFolder
		var created string
		if err := rows.Scan(&f.ID, &f.Name, &f.Root, &created); err != nil {
			return nil, err
		}
		f.CreatedAt = ParseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetMusicFolder(ctx context.Context, id int64) (*model.MusicFolder, error) {
	var f model.MusicFolder
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, root, created_at FROM music_folders WHERE id = ?`, id).Scan(&f.ID, &f.Name, &f.Root, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.CreatedAt = ParseTime(created)
	return &f, nil
}

func (s *Store) CreateMusicFolder(ctx context.Context, f *model.MusicFolder) error {
	now := Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO music_folders (name, root, created_at) VALUES (?, ?, ?)`, f.Name, f.Root, now)
	if err != nil {
		return err
	}
	f.CreatedAt = ParseTime(now)
	f.ID, err = res.LastInsertId()
	return err
}

// MusicFolderTrackCounts says how many songs (files that are there) each folder holds.
func (s *Store) MusicFolderTrackCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT folder_id, COUNT(*) FROM music_tracks WHERE missing_since IS NULL GROUP BY folder_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// DeleteMusicFolder forgets a folder and the songs read from it (the files stay on disk).
// Albums and artists nothing names any more go with them.
func (s *Store) DeleteMusicFolder(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_user_data WHERE kind = 'track' AND ref_id IN (SELECT id FROM music_tracks WHERE folder_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_folders WHERE id = ?`, id); err != nil {
		return err
	}
	if err := refreshMusicCatalog(ctx, tx, nil, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- what a scan reads and writes ----

// MusicFileState is what the scanner remembers of a file it has read before.
type MusicFileState struct {
	ID         string
	Path       string
	Size       int64
	Mtime      int64
	Identity   string
	DurationMs int64
	Lyrics     string // 'lrc' | 'txt' | 'embedded' | ''
	HasText    bool   // the file itself carries lyrics
	Missing    bool
}

// MusicFileStates lists every track row of a folder, the hidden (missing) ones too.
func (s *Store) MusicFileStates(ctx context.Context, folderID int64) ([]MusicFileState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, size, mtime, identity, duration_ms, lyrics, lyrics_text <> '', missing_since IS NOT NULL
		FROM music_tracks WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MusicFileState
	for rows.Next() {
		var f MusicFileState
		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.Mtime, &f.Identity, &f.DurationMs, &f.Lyrics, &f.HasText, &f.Missing); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MusicArtistNames lists the folded names of the artists the library has (name_key).
func (s *Store) MusicArtistNames(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name_key FROM music_artists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// MusicArtistRow is an artist as a scan names one.
type MusicArtistRow struct {
	ID, Key, Name, SortName string
}

// MusicAlbumRow is an album as a scan names one.
type MusicAlbumRow struct {
	ID, Key, Title, SortTitle string
	Artist                    MusicArtistRow // the album's artist
}

// MusicTrackRow is everything a scan writes about one file.
type MusicTrackRow struct {
	ID          string
	FolderID    int64
	Path        string
	Size        int64
	Mtime       int64
	Identity    string
	Title       string
	SortTitle   string
	Artist      string
	Credits     []MusicArtistRow // everyone credited, the main artist first (never empty)
	Album       *MusicAlbumRow   // nil: the song names no album
	AlbumArtist string
	Disc, Track int
	Year        int
	Genres      []string
	DurationMs  int64
	Container   string
	Codec       string
	Bitrate     int
	SampleRate  int
	BitDepth    int
	Channels    int
	Art         string
	Lyrics      string
	LyricsText  string
	MBRecording string
	MBAlbum     string
	GainTrack   *float64
	PeakTrack   *float64
	GainAlbum   *float64
	PeakAlbum   *float64
	Search      string
	AddedAt     time.Time // used for a new row only: a row that exists keeps its own
}

// SaveMusicTrack writes one file's row (new, or the row with this id), with the artists
// and the album it names. A row that was hidden as missing is shown again.
func (s *Store) SaveMusicTrack(ctx context.Context, t *MusicTrackRow) error {
	if len(t.Credits) == 0 {
		return errors.New("a track needs an artist")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	for _, a := range t.Credits {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_artists (id, name_key, name, sort_name, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`, a.ID, a.Key, a.Name, a.SortName, now); err != nil {
			return err
		}
	}
	var albumID sql.NullString
	if al := t.Album; al != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_artists (id, name_key, name, sort_name, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`, al.Artist.ID, al.Artist.Key, al.Artist.Name, al.Artist.SortName, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_albums (id, album_key, title, sort_title, artist_id, artist, added_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, al.ID, al.Key, al.Title, al.SortTitle, al.Artist.ID, al.Artist.Name, now, now); err != nil {
			return err
		}
		albumID = sql.NullString{String: al.ID, Valid: true}
	}
	genres := ""
	if len(t.Genres) > 0 {
		b, _ := json.Marshal(t.Genres)
		genres = string(b)
	}
	added := now
	if !t.AddedAt.IsZero() {
		added = t.AddedAt.UTC().Format(TimeFormat)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_tracks (
			id, folder_id, path, size, mtime, identity, title, sort_title, artist, artist_id, album_id, album, album_artist,
			disc, track, year, genres, duration_ms, container, codec, bitrate, sample_rate, bit_depth, channels,
			art, lyrics, lyrics_text, mb_recording, mb_album, gain_track, peak_track, gain_album, peak_album, search,
			missing_since, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			folder_id = excluded.folder_id, path = excluded.path, size = excluded.size, mtime = excluded.mtime,
			identity = excluded.identity, title = excluded.title, sort_title = excluded.sort_title,
			artist = excluded.artist, artist_id = excluded.artist_id, album_id = excluded.album_id, album = excluded.album,
			album_artist = excluded.album_artist, disc = excluded.disc, track = excluded.track, year = excluded.year,
			genres = excluded.genres, duration_ms = excluded.duration_ms, container = excluded.container,
			codec = excluded.codec, bitrate = excluded.bitrate, sample_rate = excluded.sample_rate,
			bit_depth = excluded.bit_depth, channels = excluded.channels, art = excluded.art, lyrics = excluded.lyrics,
			lyrics_text = excluded.lyrics_text, mb_recording = excluded.mb_recording, mb_album = excluded.mb_album,
			gain_track = excluded.gain_track, peak_track = excluded.peak_track, gain_album = excluded.gain_album,
			peak_album = excluded.peak_album, search = excluded.search, missing_since = NULL, updated_at = excluded.updated_at,
			loudness_tries = CASE WHEN music_tracks.mtime <> excluded.mtime OR music_tracks.size <> excluded.size THEN 0 ELSE music_tracks.loudness_tries END`,
		t.ID, t.FolderID, t.Path, t.Size, t.Mtime, t.Identity, t.Title, t.SortTitle, t.Artist, t.Credits[0].ID, albumID, t.Album.title(), t.AlbumArtist,
		t.Disc, t.Track, t.Year, genres, t.DurationMs, t.Container, t.Codec, t.Bitrate, t.SampleRate, t.BitDepth, t.Channels,
		t.Art, t.Lyrics, t.LyricsText, t.MBRecording, t.MBAlbum, nullF(t.GainTrack), nullF(t.PeakTrack), nullF(t.GainAlbum), nullF(t.PeakAlbum), t.Search,
		added, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_track_artists WHERE track_id = ?`, t.ID); err != nil {
		return err
	}
	for i, a := range t.Credits {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO music_track_artists (track_id, artist_id, position) VALUES (?, ?, ?)`, t.ID, a.ID, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *MusicAlbumRow) title() string {
	if a == nil {
		return ""
	}
	return a.Title
}

func nullF(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

// SetMusicTrackLyrics records where a track's lyrics are now (a lyrics file appeared
// beside it, or went).
func (s *Store) SetMusicTrackLyrics(ctx context.Context, id, kind string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music_tracks SET lyrics = ? WHERE id = ?`, kind, id)
	return err
}

// MarkMusicMissing hides tracks whose files are gone. The rows stay: a file that comes
// back (a drive mounted again, a folder moved back) is the same song, played as often
// and still a favourite.
func (s *Store) MarkMusicMissing(ctx context.Context, ids []string) error {
	now := Now()
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE music_tracks SET missing_since = ? WHERE id = ? AND missing_since IS NULL`, now, id); err != nil {
			return err
		}
	}
	return nil
}

// PurgeMusicMissing removes the rows of files gone since before `before`, with what
// profiles noted about them, and returns how many went.
func (s *Store) PurgeMusicMissing(ctx context.Context, folderID int64, before time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	cut := before.UTC().Format(TimeFormat)
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_user_data WHERE kind = 'track' AND ref_id IN
		(SELECT id FROM music_tracks WHERE folder_id = ? AND missing_since IS NOT NULL AND missing_since < ?)`, folderID, cut); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM music_tracks WHERE folder_id = ? AND missing_since IS NOT NULL AND missing_since < ?`, folderID, cut)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), tx.Commit()
}

// MoveMusicUserData gives what profiles noted about one track to another (the file was
// replaced by another file of the same song while both were there): plays are added,
// a favourite stays a favourite. The first track's notes are removed.
func (s *Store) MoveMusicUserData(ctx context.Context, fromID, toID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_user_data (scope_id, kind, ref_id, play_count, last_played_at, is_favorite, favorited_at)
		SELECT scope_id, 'track', ?, play_count, last_played_at, is_favorite, favorited_at FROM music_user_data WHERE kind = 'track' AND ref_id = ?
		ON CONFLICT(scope_id, kind, ref_id) DO UPDATE SET
			play_count = music_user_data.play_count + excluded.play_count,
			last_played_at = CASE WHEN excluded.last_played_at IS NULL OR music_user_data.last_played_at > excluded.last_played_at
				THEN music_user_data.last_played_at ELSE excluded.last_played_at END,
			is_favorite = MAX(music_user_data.is_favorite, excluded.is_favorite),
			favorited_at = COALESCE(music_user_data.favorited_at, excluded.favorited_at)`, toID, fromID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_user_data WHERE kind = 'track' AND ref_id = ?`, fromID); err != nil {
		return err
	}
	// Playlists that hold the song go on holding it, and its listens are the heir's.
	if err := movePlaylistEntries(ctx, tx, fromID, toID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE music_plays SET track_id = ? WHERE track_id = ?`, toID, fromID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteMusicTrack removes one track row (its notes were moved or are not wanted).
func (s *Store) DeleteMusicTrack(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM music_tracks WHERE id = ?`, id)
	return err
}

// RenameMusicArtist sets the spelling an artist is shown with (the artist folder's).
func (s *Store) RenameMusicArtist(ctx context.Context, id, name, sortName string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music_artists SET name = ?, sort_name = ? WHERE id = ? AND (name <> ? OR sort_name <> ?)`, name, sortName, id, name, sortName)
	return err
}

// RefreshMusicCatalog brings albums and artists in line with the tracks after a scan:
// counts, lengths and pictures are those of the files that are there, and albums and
// artists that no track names any more are removed. albumArt and artistArt are pictures
// found in the folders (album id / artist id to hash); they come before a track's own.
func (s *Store) RefreshMusicCatalog(ctx context.Context, albumArt, artistArt map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := refreshMusicCatalog(ctx, tx, albumArt, artistArt); err != nil {
		return err
	}
	return tx.Commit()
}

func refreshMusicCatalog(ctx context.Context, tx *sql.Tx, albumArt, artistArt map[string]string) error {
	const present = `t.missing_since IS NULL`
	for _, q := range []string{
		`DELETE FROM music_albums WHERE NOT EXISTS (SELECT 1 FROM music_tracks t WHERE t.album_id = music_albums.id)`,
		`UPDATE music_albums SET
			track_count = (SELECT COUNT(*) FROM music_tracks t WHERE t.album_id = music_albums.id AND ` + present + `),
			duration_ms = (SELECT COALESCE(SUM(t.duration_ms), 0) FROM music_tracks t WHERE t.album_id = music_albums.id AND ` + present + `),
			year = (SELECT COALESCE(MAX(t.year), 0) FROM music_tracks t WHERE t.album_id = music_albums.id AND ` + present + `),
			added_at = COALESCE((SELECT MAX(t.added_at) FROM music_tracks t WHERE t.album_id = music_albums.id AND ` + present + `), added_at),
			art = COALESCE((SELECT t.art FROM music_tracks t WHERE t.album_id = music_albums.id AND ` + present + ` AND t.art <> ''
				GROUP BY t.art ORDER BY COUNT(*) DESC, MIN(t.disc), MIN(t.track) LIMIT 1), '')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	for id, hash := range albumArt {
		if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET art = ? WHERE id = ?`, hash, id); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`DELETE FROM music_artists WHERE
			NOT EXISTS (SELECT 1 FROM music_tracks t WHERE t.artist_id = music_artists.id)
			AND NOT EXISTS (SELECT 1 FROM music_track_artists ta WHERE ta.artist_id = music_artists.id)
			AND NOT EXISTS (SELECT 1 FROM music_albums al WHERE al.artist_id = music_artists.id)`,
		// An artist is pictured by the newest song they lead that has a picture, else by
		// the newest they are a guest on.
		`UPDATE music_artists SET art = COALESCE(
			(SELECT CASE WHEN t.art <> '' THEN t.art ELSE al.art END FROM music_tracks t LEFT JOIN music_albums al ON al.id = t.album_id
				WHERE t.artist_id = music_artists.id AND ` + present + ` AND (t.art <> '' OR COALESCE(al.art, '') <> '')
				ORDER BY t.added_at DESC, t.id LIMIT 1),
			(SELECT CASE WHEN t.art <> '' THEN t.art ELSE al.art END FROM music_track_artists ta JOIN music_tracks t ON t.id = ta.track_id
				LEFT JOIN music_albums al ON al.id = t.album_id
				WHERE ta.artist_id = music_artists.id AND ` + present + ` AND (t.art <> '' OR COALESCE(al.art, '') <> '')
				ORDER BY t.added_at DESC, t.id LIMIT 1),
			'')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	for id, hash := range artistArt {
		if _, err := tx.ExecContext(ctx, `UPDATE music_artists SET art = ? WHERE id = ?`, hash, id); err != nil {
			return err
		}
	}
	return nil
}

// MusicArtInUse lists the hashes of every picture a track, album or artist shows.
func (s *Store) MusicArtInUse(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT art FROM music_tracks WHERE art <> ''
		UNION SELECT art FROM music_albums WHERE art <> '' UNION SELECT art FROM music_artists WHERE art <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out[h] = true
	}
	return out, rows.Err()
}

// ---- what the clients read ----

// MusicCounts is how much music there is (files that are there).
type MusicCounts struct {
	Tracks  int `json:"tracks"`
	Albums  int `json:"albums"`
	Artists int `json:"artists"`
}

// albumMinTracks: a song that names an album is not by itself an album. A library of
// single songs would otherwise show hundreds of one-song "albums"; an album is listed
// once two of its songs are there.
const albumMinTracks = 2

func (s *Store) MusicCounts(ctx context.Context) (MusicCounts, error) {
	var c MusicCounts
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM music_tracks WHERE missing_since IS NULL),
		(SELECT COUNT(*) FROM music_albums WHERE track_count >= ?),
		(SELECT COUNT(*) FROM music_artists a WHERE EXISTS (SELECT 1 FROM music_tracks t WHERE t.artist_id = a.id AND t.missing_since IS NULL))`, albumMinTracks).
		Scan(&c.Tracks, &c.Albums, &c.Artists)
	return c, err
}

// MusicTrackQuery chooses and orders tracks.
type MusicTrackQuery struct {
	IDs      []string // these tracks, in this order (the other filters still apply)
	ArtistID string   // everything the artist is credited on
	AlbumID  string
	Favorite bool
	Played   bool   // only tracks the profile has played
	Search   string // folded words, all of which must be found
	// Sort: "title" (default), "artist", "album" (disc and track order), "added", "plays",
	// "recent" (last played), "favorited".
	Sort   string
	Limit  int
	Offset int
}

const musicTrackCols = `t.id, t.title, t.artist, t.artist_id, COALESCE(t.album_id, ''), t.album, t.album_artist, t.disc, t.track, t.year,
	t.duration_ms, t.container, t.codec, t.bitrate, t.sample_rate, t.bit_depth, t.channels, t.size,
	CASE WHEN t.art <> '' THEN t.art ELSE COALESCE(al.art, '') END, t.lyrics, t.gain_track, t.gain_album, t.added_at, t.path, t.mtime,
	t.loudness, t.true_peak, al.loudness, al.true_peak,
	COALESCE(u.is_favorite, 0), COALESCE(u.play_count, 0), u.last_played_at`

const musicTrackFrom = ` FROM music_tracks t
	LEFT JOIN music_albums al ON al.id = t.album_id
	LEFT JOIN music_artists ar ON ar.id = t.artist_id
	LEFT JOIN music_user_data u ON u.scope_id = ? AND u.kind = 'track' AND u.ref_id = t.id`

func scanMusicTrack(rows interface{ Scan(...any) error }) (model.MusicTrack, error) {
	var t model.MusicTrack
	var gt, ga, loud, peak, albumLoud, albumPeak sql.NullFloat64
	var added string
	var last sql.NullString
	var fav int
	if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.ArtistID, &t.AlbumID, &t.Album, &t.AlbumArtist, &t.Disc, &t.Track, &t.Year,
		&t.DurationMs, &t.Container, &t.Codec, &t.Bitrate, &t.SampleRate, &t.BitDepth, &t.Channels, &t.Size,
		&t.Art, &t.Lyrics, &gt, &ga, &added, &t.Path, &t.Mtime, &loud, &peak, &albumLoud, &albumPeak, &fav, &t.PlayCount, &last); err != nil {
		return t, err
	}
	if loud.Valid {
		t.Loudness = &loud.Float64
	}
	if peak.Valid {
		t.Peak = &peak.Float64
	}
	if albumLoud.Valid {
		t.AlbumLoudness = &albumLoud.Float64
	}
	if albumPeak.Valid {
		t.AlbumPeak = &albumPeak.Float64
	}
	if gt.Valid {
		t.GainTrack = &gt.Float64
	}
	if ga.Valid {
		t.GainAlbum = &ga.Float64
	}
	t.AddedAt = ParseTime(added)
	if last.Valid && last.String != "" {
		lt := ParseTime(last.String)
		t.LastPlayedAt = &lt
	}
	t.Favorite = fav == 1
	t.Lossless = model.LosslessCodec(t.Codec)
	return t, nil
}

// ListMusicTracks lists the tracks a query chooses, for one profile, and how many there
// are in all (before Limit and Offset).
func (s *Store) ListMusicTracks(ctx context.Context, scope string, q MusicTrackQuery) ([]model.MusicTrack, int, error) {
	where := []string{`t.missing_since IS NULL`}
	args := []any{scope}
	if len(q.IDs) > 0 {
		where = append(where, `t.id IN (`+placeholders(len(q.IDs))+`)`)
		for _, id := range q.IDs {
			args = append(args, id)
		}
	}
	if q.ArtistID != "" {
		where = append(where, `t.id IN (SELECT track_id FROM music_track_artists WHERE artist_id = ?)`)
		args = append(args, q.ArtistID)
	}
	if q.AlbumID != "" {
		where = append(where, `t.album_id = ?`)
		args = append(args, q.AlbumID)
	}
	if q.Favorite {
		where = append(where, `u.is_favorite = 1`)
	}
	if q.Played || q.Sort == "recent" {
		where = append(where, `u.last_played_at IS NOT NULL`)
	}
	for _, word := range strings.Fields(q.Search) {
		where = append(where, `t.search LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(word)+"%")
	}
	cond := ` WHERE ` + strings.Join(where, ` AND `)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+musicTrackFrom+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `t.sort_title, ar.sort_name, t.id`
	switch q.Sort {
	case "artist":
		order = `ar.sort_name, COALESCE(al.sort_title, ''), t.disc, t.track, t.sort_title, t.id`
	case "album":
		order = `t.disc, t.track, t.sort_title, t.id`
	case "added":
		order = `t.added_at DESC, COALESCE(al.sort_title, ''), t.disc, t.track, t.id`
	case "plays":
		order = `COALESCE(u.play_count, 0) DESC, u.last_played_at DESC, t.sort_title, t.id`
	case "recent":
		order = `u.last_played_at DESC, t.id`
	case "favorited":
		order = `u.favorited_at DESC, t.sort_title, t.id`
	}
	query := `SELECT ` + musicTrackCols + musicTrackFrom + cond + ` ORDER BY ` + order
	if q.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, q.Limit, q.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.MusicTrack
	for rows.Next() {
		t, err := scanMusicTrack(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if len(q.IDs) > 0 && q.Sort == "" {
		byID := make(map[string]model.MusicTrack, len(out))
		for _, t := range out {
			byID[t.ID] = t
		}
		out = out[:0]
		for _, id := range q.IDs {
			if t, ok := byID[id]; ok {
				out = append(out, t)
				delete(byID, id)
			}
		}
	}
	if err := s.fillMusicCredits(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// fillMusicCredits sets each track's Artists (everyone credited, in order).
func (s *Store) fillMusicCredits(ctx context.Context, tracks []model.MusicTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	at := make(map[string]int, len(tracks))
	for i := range tracks {
		at[tracks[i].ID] = i
	}
	// In chunks: SQLite takes a limited number of values in one statement.
	ids := make([]string, 0, len(tracks))
	for id := range at {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(len(ids), start+500)]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT ta.track_id, a.id, a.name FROM music_track_artists ta JOIN music_artists a ON a.id = ta.artist_id
			WHERE ta.track_id IN (`+placeholders(len(chunk))+`) ORDER BY ta.track_id, ta.position`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var trackID string
			var ref model.ArtistRef
			if err := rows.Scan(&trackID, &ref.ID, &ref.Name); err != nil {
				rows.Close()
				return err
			}
			i := at[trackID]
			tracks[i].Artists = append(tracks[i].Artists, ref)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

// GetMusicTrack is one track for a profile (nil when there is none, or its file is gone),
// with the lyrics the file carries.
func (s *Store) GetMusicTrack(ctx context.Context, scope, id string) (*model.MusicTrack, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+musicTrackCols+musicTrackFrom+` WHERE t.id = ? AND t.missing_since IS NULL`, scope, id)
	t, err := scanMusicTrack(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT lyrics_text FROM music_tracks WHERE id = ?`, id).Scan(&t.LyricsText); err != nil {
		return nil, err
	}
	one := []model.MusicTrack{t}
	if err := s.fillMusicCredits(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// MusicArtistQuery chooses artists.
type MusicArtistQuery struct {
	Search string
	// Guests: also the artists who only appear on other artists' songs. Without it the
	// list is of the artists who lead at least one song.
	Guests   bool
	Favorite bool
	Limit    int
	Offset   int
}

var musicArtistCols = `a.id, a.name, a.art,
	(SELECT COUNT(*) FROM music_track_artists ta JOIN music_tracks t ON t.id = ta.track_id WHERE ta.artist_id = a.id AND t.missing_since IS NULL),
	(SELECT COUNT(*) FROM music_albums al WHERE al.artist_id = a.id AND al.track_count >= ` + strconv.Itoa(albumMinTracks) + `),
	COALESCE(u.is_favorite, 0)`

const musicArtistFrom = ` FROM music_artists a LEFT JOIN music_user_data u ON u.scope_id = ? AND u.kind = 'artist' AND u.ref_id = a.id`

func scanMusicArtist(rows interface{ Scan(...any) error }) (model.MusicArtist, error) {
	var a model.MusicArtist
	var fav int
	err := rows.Scan(&a.ID, &a.Name, &a.Art, &a.TrackCount, &a.AlbumCount, &fav)
	a.Favorite = fav == 1
	return a, err
}

// ListMusicArtists lists artists by name, and how many the query chooses in all.
func (s *Store) ListMusicArtists(ctx context.Context, scope string, q MusicArtistQuery) ([]model.MusicArtist, int, error) {
	where := []string{}
	args := []any{scope}
	if q.Guests {
		where = append(where, `EXISTS (SELECT 1 FROM music_track_artists ta JOIN music_tracks t ON t.id = ta.track_id WHERE ta.artist_id = a.id AND t.missing_since IS NULL)`)
	} else {
		where = append(where, `EXISTS (SELECT 1 FROM music_tracks t WHERE t.artist_id = a.id AND t.missing_since IS NULL)`)
	}
	if q.Favorite {
		where = append(where, `u.is_favorite = 1`)
	}
	for _, word := range strings.Fields(q.Search) {
		where = append(where, `a.name_key LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(word)+"%")
	}
	cond := ` WHERE ` + strings.Join(where, ` AND `)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+musicArtistFrom+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + musicArtistCols + musicArtistFrom + cond + ` ORDER BY a.sort_name, a.id`
	if q.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, q.Limit, q.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.MusicArtist
	for rows.Next() {
		a, err := scanMusicArtist(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// GetMusicArtist is one artist (nil when there is none).
func (s *Store) GetMusicArtist(ctx context.Context, scope, id string) (*model.MusicArtist, error) {
	a, err := scanMusicArtist(s.db.QueryRowContext(ctx, `SELECT `+musicArtistCols+musicArtistFrom+` WHERE a.id = ?`, scope, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MusicAlbumQuery chooses and orders albums.
type MusicAlbumQuery struct {
	IDs      []string // only these albums
	ArtistID string
	Search   string
	Favorite bool
	// Singles: also the albums only one song of which is there (see albumMinTracks).
	Singles bool
	// Sort: "title" (default), "added", "year", "artist".
	Sort   string
	Limit  int
	Offset int
}

const musicAlbumCols = `al.id, al.title, al.artist_id, COALESCE(ar.name, al.artist), al.year, al.art, al.track_count, al.duration_ms, al.added_at, COALESCE(u.is_favorite, 0)`

const musicAlbumFrom = ` FROM music_albums al LEFT JOIN music_artists ar ON ar.id = al.artist_id
	LEFT JOIN music_user_data u ON u.scope_id = ? AND u.kind = 'album' AND u.ref_id = al.id`

func scanMusicAlbum(rows interface{ Scan(...any) error }) (model.MusicAlbum, error) {
	var a model.MusicAlbum
	var added string
	var fav int
	err := rows.Scan(&a.ID, &a.Title, &a.ArtistID, &a.Artist, &a.Year, &a.Art, &a.TrackCount, &a.DurationMs, &added, &fav)
	a.AddedAt = ParseTime(added)
	a.Favorite = fav == 1
	return a, err
}

// ListMusicAlbums lists albums, and how many the query chooses in all.
func (s *Store) ListMusicAlbums(ctx context.Context, scope string, q MusicAlbumQuery) ([]model.MusicAlbum, int, error) {
	minTracks := albumMinTracks
	if q.Singles {
		minTracks = 1
	}
	where := []string{`al.track_count >= ?`}
	args := []any{scope, minTracks}
	if len(q.IDs) > 0 {
		where = append(where, `al.id IN (`+placeholders(len(q.IDs))+`)`)
		args = append(args, anys(q.IDs)...)
	}
	if q.ArtistID != "" {
		where = append(where, `al.artist_id = ?`)
		args = append(args, q.ArtistID)
	}
	if q.Favorite {
		where = append(where, `u.is_favorite = 1`)
	}
	for _, word := range strings.Fields(q.Search) {
		where = append(where, `(al.album_key LIKE ? ESCAPE '\')`)
		args = append(args, "%"+likeEscape(word)+"%")
	}
	cond := ` WHERE ` + strings.Join(where, ` AND `)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+musicAlbumFrom+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `al.sort_title, al.id`
	switch q.Sort {
	case "added":
		order = `al.added_at DESC, al.id`
	case "year":
		order = `al.year DESC, al.sort_title, al.id`
	case "artist":
		order = `COALESCE(ar.sort_name, ''), al.year, al.sort_title, al.id`
	}
	query := `SELECT ` + musicAlbumCols + musicAlbumFrom + cond + ` ORDER BY ` + order
	if q.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, q.Limit, q.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.MusicAlbum
	for rows.Next() {
		a, err := scanMusicAlbum(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// GetMusicAlbum is one album (nil when there is none or none of its songs is there).
func (s *Store) GetMusicAlbum(ctx context.Context, scope, id string) (*model.MusicAlbum, error) {
	a, err := scanMusicAlbum(s.db.QueryRowContext(ctx, `SELECT `+musicAlbumCols+musicAlbumFrom+` WHERE al.id = ? AND al.track_count >= 1`, scope, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ---- what a profile does ----

// SetMusicFavorite marks (or unmarks) a track, album or artist as a profile's favourite.
func (s *Store) SetMusicFavorite(ctx context.Context, scope, kind, id string, on bool) error {
	var at sql.NullString
	if on {
		at = sql.NullString{String: Now(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO music_user_data (scope_id, kind, ref_id, is_favorite, favorited_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope_id, kind, ref_id) DO UPDATE SET is_favorite = excluded.is_favorite, favorited_at = excluded.favorited_at`,
		scope, kind, id, boolInt(on), at)
	return err
}

// RecordMusicPlay counts one listen of a track by a profile, kept as listened to at `at`.
// `reported` is the time the player itself gave for the listen (Unix ms): a report with
// the same one is the same listen sent again (a retry), and is not counted twice;
// counted says whether this report was new. 0 means the player gave no time: every such
// report is a listen of its own.
func (s *Store) RecordMusicPlay(ctx context.Context, scope, trackID string, at time.Time, reported int64) (counted bool, err error) {
	stamp := at.UTC().Format(TimeFormat)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	own := reported > 0
	if !own {
		reported = at.UnixMilli()
	}
	for tries := 0; ; tries++ {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO music_plays (scope_id, track_id, reported, played_at) VALUES (?, ?, ?, ?)`, scope, trackID, reported, stamp)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			break
		}
		// A report that names its time is the one already counted. One that names none
		// only met another of the same millisecond: it takes the next free one.
		if own || tries > 1000 {
			return false, nil
		}
		reported++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_user_data (scope_id, kind, ref_id, play_count, last_played_at) VALUES (?, 'track', ?, 1, ?)
		ON CONFLICT(scope_id, kind, ref_id) DO UPDATE SET play_count = music_user_data.play_count + 1,
			last_played_at = CASE WHEN music_user_data.last_played_at IS NULL OR music_user_data.last_played_at < excluded.last_played_at
				THEN excluded.last_played_at ELSE music_user_data.last_played_at END`, scope, trackID, stamp); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// MusicPlayCount is how often a profile has listened to a track.
func (s *Store) MusicPlayCount(ctx context.Context, scope, trackID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_plays WHERE scope_id = ? AND track_id = ?`, scope, trackID).Scan(&n)
	return n, err
}

// ---- loudness ----

// MusicUnmeasured is a track whose loudness is not measured for the file as it is now.
type MusicUnmeasured struct {
	ID      string
	Path    string
	Mtime   int64
	AlbumID string // "" when the song names no album
}

// MusicLoudnessMaxTries is how many failed measurements a file gets before it is left alone.
const MusicLoudnessMaxTries = 3

// musicToMeasure: files that are there, not measured as they are now (or measured before
// their blocks were kept), and not given up on.
const musicToMeasure = `missing_since IS NULL AND (loudness_mtime <> mtime OR loudness_blocks IS NULL) AND loudness_tries < ?`

// MusicTracksToMeasure lists the tracks whose loudness has to be measured, the ones that
// came in last first. All of them at once: a pass goes down the list one time, however
// many there are and however many fail on the way.
func (s *Store) MusicTracksToMeasure(ctx context.Context) ([]MusicUnmeasured, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, mtime, COALESCE(album_id, '') FROM music_tracks WHERE `+musicToMeasure+` ORDER BY added_at DESC, id`, MusicLoudnessMaxTries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MusicUnmeasured
	for rows.Next() {
		var u MusicUnmeasured
		if err := rows.Scan(&u.ID, &u.Path, &u.Mtime, &u.AlbumID); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetMusicLoudness records a measurement for the file as it was at mtime: its loudness,
// its true peak and how loud its blocks are (music.Blocks, packed). A file that changed
// meanwhile keeps being listed for measuring.
func (s *Store) SetMusicLoudness(ctx context.Context, id string, mtime int64, lufs, peak float64, blocks []byte) error {
	if blocks == nil {
		blocks = []byte{} // measured, with no blocks (shorter than one): not "never measured"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE music_tracks SET loudness = ?, true_peak = ?, loudness_blocks = ?, loudness_mtime = ?, loudness_tries = 0 WHERE id = ?`, lufs, peak, blocks, mtime, id)
	return err
}

// MusicLoudnessFailed notes a measurement that failed.
func (s *Store) MusicLoudnessFailed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music_tracks SET loudness_tries = loudness_tries + 1 WHERE id = ?`, id)
	return err
}

// MusicLoudnessTries is how many measurements of a track have failed (for tests).
func (s *Store) MusicLoudnessTries(ctx context.Context, id string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT loudness_tries FROM music_tracks WHERE id = ?`, id).Scan(&n)
	return n, err
}

// MusicLoudnessCounts says how many songs are measured as they are now, of how many there are.
func (s *Store) MusicLoudnessCounts(ctx context.Context) (measured, total int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(loudness IS NOT NULL AND loudness_mtime = mtime AND loudness_blocks IS NOT NULL), 0), COUNT(*) FROM music_tracks WHERE missing_since IS NULL`).Scan(&measured, &total)
	return
}

// MusicAlbumTrackLoudness is one song's part in its album's loudness.
type MusicAlbumTrackLoudness struct {
	AlbumID  string
	Measured bool    // measured as its file is now
	GivenUp  bool    // not measured, and not going to be until the file changes
	Peak     float64 // true peak, when measured
	Blocks   []byte  // how loud its blocks are, packed, when measured
}

// MusicAlbumLoudnessInputs hands over, album by album, the songs that are there of the
// albums named (of every album when none are named), with what was measured of each.
func (s *Store) MusicAlbumLoudnessInputs(ctx context.Context, albumIDs []string, each func(MusicAlbumTrackLoudness) error) error {
	const cols = `SELECT album_id, loudness IS NOT NULL AND loudness_mtime = mtime AND loudness_blocks IS NOT NULL, loudness_tries >= ?, COALESCE(true_peak, 0), loudness_blocks FROM music_tracks WHERE album_id IS NOT NULL AND missing_since IS NULL`
	read := func(query string, args ...any) error {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t MusicAlbumTrackLoudness
			if err := rows.Scan(&t.AlbumID, &t.Measured, &t.GivenUp, &t.Peak, &t.Blocks); err != nil {
				return err
			}
			if err := each(t); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if len(albumIDs) == 0 {
		return read(cols+` ORDER BY album_id`, MusicLoudnessMaxTries)
	}
	// In parts: a database takes only so many values in one question.
	for len(albumIDs) > 0 {
		part := albumIDs[:min(len(albumIDs), 400)]
		albumIDs = albumIDs[len(part):]
		args := []any{MusicLoudnessMaxTries}
		for _, id := range part {
			args = append(args, id)
		}
		if err := read(cols+` AND album_id IN (`+placeholders(len(part))+`) ORDER BY album_id`, args...); err != nil {
			return err
		}
	}
	return nil
}

// MusicAlbumLevel is an album's loudness and true peak (nil: it has none yet).
type MusicAlbumLevel struct {
	Loudness *float64
	Peak     *float64
}

// SetMusicAlbumLoudness sets the albums named. With others set, every other album loses
// its figures (they were worked out for all, and those not named have none).
func (s *Store) SetMusicAlbumLoudness(ctx context.Context, byAlbum map[string]MusicAlbumLevel, others bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if others {
		if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET loudness = NULL, true_peak = NULL WHERE loudness IS NOT NULL OR true_peak IS NOT NULL`); err != nil {
			return err
		}
	}
	for id, l := range byAlbum {
		if others && l.Loudness == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET loudness = ?, true_peak = ? WHERE id = ?`, l.Loudness, l.Peak, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MusicExists reports whether there is a track, album or artist with this id.
func (s *Store) MusicExists(ctx context.Context, kind, id string) (bool, error) {
	table := map[string]string{"track": "music_tracks", "album": "music_albums", "artist": "music_artists"}[kind]
	if table == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// likeEscape makes a search word safe inside a LIKE pattern (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
