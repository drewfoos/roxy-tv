package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SeasonCount is how much of one season the library holds.
type SeasonCount struct {
	Have  int `json:"have"`  // episodes with a file
	Total int `json:"total"` // episodes known (files + placeholders)
}

// SeasonAvailability counts, per aired season number, how many of a series' episodes
// have a file and how many are known at all (provider placeholders included).
func (s *Store) SeasonAvailability(ctx context.Context, seriesID string) (map[int]SeasonCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(e.season_aired, 0), COUNT(*),
		       SUM(CASE WHEN EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id) THEN 1 ELSE 0 END)
		FROM items e JOIN items sn ON e.parent_id = sn.id
		WHERE sn.parent_id = ? AND e.type = 'episode'
		GROUP BY COALESCE(e.season_aired, 0)`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]SeasonCount{}
	for rows.Next() {
		var n int
		var c SeasonCount
		if err := rows.Scan(&n, &c.Total, &c.Have); err != nil {
			return nil, err
		}
		out[n] = c
	}
	return out, rows.Err()
}

// Request is a Discover request row.
type Request struct {
	ID         string `json:"id"`
	MediaType  string `json:"type"` // tv | movie
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	PosterPath string `json:"-"`
	UserID     string `json:"userId"`
	UserName   string `json:"userName,omitempty"`
	Status     string `json:"status"` // pending | sent | failed | approval (basic user, waiting for an admin) | declined
	// Seasons asked for (series only); empty = the whole series.
	Seasons []int `json:"seasons,omitempty"`
	// Individual episodes asked for (specials), as "S0E5" codes.
	Episodes []string `json:"episodes,omitempty"`
	// FilmAuto: the request asked for the films between its seasons too (the button
	// named them), and brings such films as they are announced. FilmSeasons are the
	// seasons it covered, by AniList id: only films between two of THESE come by
	// themselves, however the seasons are later numbered, split or added to.
	FilmAuto    bool   `json:"filmAuto,omitempty"`
	FilmSeasons []int  `json:"filmSeasons,omitempty"`
	ArrID       int    `json:"arrId,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

const requestCols = `r.id, r.media_type, r.tmdb_id, r.title, COALESCE(r.year, 0), COALESCE(r.poster_path, ''), r.user_id, COALESCE(NULLIF(r.requested_by, ''), u.name, ''), r.status, COALESCE(r.arr_id, 0), COALESCE(r.error, ''), r.created_at, r.updated_at, COALESCE(r.seasons, ''), COALESCE(r.episodes, ''), r.film_scope`

func scanRequest(row interface{ Scan(...any) error }) (*Request, error) {
	var r Request
	var seasons, episodes, films string
	if err := row.Scan(&r.ID, &r.MediaType, &r.TMDBID, &r.Title, &r.Year, &r.PosterPath, &r.UserID, &r.UserName, &r.Status, &r.ArrID, &r.Error, &r.CreatedAt, &r.UpdatedAt, &seasons, &episodes, &films); err != nil {
		return nil, err
	}
	if films != "" {
		var fs filmScope
		if json.Unmarshal([]byte(films), &fs) == nil {
			r.FilmAuto, r.FilmSeasons = fs.Auto, fs.Seasons
		}
	}
	if seasons != "" {
		_ = json.Unmarshal([]byte(seasons), &r.Seasons)
	}
	if episodes != "" {
		_ = json.Unmarshal([]byte(episodes), &r.Episodes)
	}
	return &r, nil
}

// filmScope is how FilmAuto and FilmSeasons are kept (requests.film_scope).
type filmScope struct {
	Auto    bool  `json:"auto"`
	Seasons []int `json:"seasons,omitempty"`
}

func (s *Store) GetRequest(ctx context.Context, mediaType string, tmdbID int) (*Request, error) {
	r, err := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+requestCols+` FROM requests r LEFT JOIN users u ON u.id = r.user_id WHERE r.media_type = ? AND r.tmdb_id = ?`, mediaType, tmdbID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// GetRequestByID returns one request, or nil.
func (s *Store) GetRequestByID(ctx context.Context, id string) (*Request, error) {
	r, err := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+requestCols+` FROM requests r LEFT JOIN users u ON u.id = r.user_id WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// ListRequests returns every request, newest first (all users: the household sees what was asked for).
func (s *Store) ListRequests(ctx context.Context) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestCols+` FROM requests r LEFT JOIN users u ON u.id = r.user_id ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// RequestsByTMDB maps "type:tmdbId" to the request for a batch of ids.
func (s *Store) RequestsByTMDB(ctx context.Context) (map[string]Request, error) {
	all, err := s.ListRequests(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Request, len(all))
	for _, r := range all {
		out[r.MediaType+":"+itoa(r.TMDBID)] = r
	}
	return out, nil
}

func (s *Store) UpsertRequest(ctx context.Context, r *Request) error {
	now := Now()
	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	var seasons sql.NullString
	if len(r.Seasons) > 0 {
		b, _ := json.Marshal(r.Seasons)
		seasons = sql.NullString{String: string(b), Valid: true}
	}
	var episodes sql.NullString
	if len(r.Episodes) > 0 {
		b, _ := json.Marshal(r.Episodes)
		episodes = sql.NullString{String: string(b), Valid: true}
	}
	films := ""
	if r.FilmAuto || len(r.FilmSeasons) > 0 {
		b, _ := json.Marshal(filmScope{Auto: r.FilmAuto, Seasons: r.FilmSeasons})
		films = string(b)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests (id, media_type, tmdb_id, title, year, poster_path, user_id, status, arr_id, error, created_at, updated_at, seasons, requested_by, episodes, film_scope)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(media_type, tmdb_id) DO UPDATE SET status = excluded.status, arr_id = excluded.arr_id, error = excluded.error, updated_at = excluded.updated_at, seasons = excluded.seasons, episodes = excluded.episodes, film_scope = excluded.film_scope`,
		r.ID, r.MediaType, r.TMDBID, r.Title, nullInt(r.Year), nullStr(r.PosterPath), r.UserID, r.Status, nullInt(r.ArrID), nullStr(r.Error), r.CreatedAt, r.UpdatedAt, seasons, nullStr(r.UserName), episodes, films)
	return err
}

func (s *Store) DeleteRequest(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM requests WHERE id = ?`, id)
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// EpisodeFiles lists, per aired season, the episode numbers that have a file, for a series.
func (s *Store) EpisodeFiles(ctx context.Context, seriesID string) (map[int]map[int]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(e.season_aired, 0), COALESCE(e.index_aired, 0)
		FROM items e JOIN items sn ON e.parent_id = sn.id
		WHERE sn.parent_id = ? AND e.type = 'episode'
		  AND EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id)`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]map[int]bool{}
	for rows.Next() {
		var sn, ep int
		if err := rows.Scan(&sn, &ep); err != nil {
			return nil, err
		}
		if out[sn] == nil {
			out[sn] = map[int]bool{}
		}
		out[sn][ep] = true
	}
	return out, rows.Err()
}

// SpecialFile is a special (aired season 0) the library has a file of.
type SpecialFile struct {
	Number  int // as its file numbers it (Sonarr's and TheTVDB's count)
	Title   string
	AirDate string // YYYY-MM-DD, or empty
}

// SpecialFiles lists a series' specials that have a file, with what is needed to tell
// which special each one is: a catalogue that counts specials differently (TMDB) cannot
// go by the number (see EpisodeFiles for the regular seasons, where it can).
func (s *Store) SpecialFiles(ctx context.Context, seriesID string) ([]SpecialFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(e.index_aired, 0), e.title, COALESCE(e.premiere_date, '')
		FROM items e JOIN items sn ON e.parent_id = sn.id
		WHERE sn.parent_id = ? AND e.type = 'episode' AND COALESCE(e.season_aired, 0) = 0
		  AND EXISTS (SELECT 1 FROM media_sources ms WHERE ms.item_id = e.id)
		ORDER BY 1`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpecialFile
	for rows.Next() {
		var f SpecialFile
		if err := rows.Scan(&f.Number, &f.Title, &f.AirDate); err != nil {
			return nil, err
		}
		if len(f.AirDate) > 10 {
			f.AirDate = f.AirDate[:10]
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
