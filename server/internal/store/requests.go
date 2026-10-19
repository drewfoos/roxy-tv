package store

import (
	"context"
	"database/sql"
	"errors"
)

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
	Status     string `json:"status"` // pending | sent | failed (available is derived from the library)
	ArrID      int    `json:"arrId,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

const requestCols = `r.id, r.media_type, r.tmdb_id, r.title, COALESCE(r.year, 0), COALESCE(r.poster_path, ''), r.user_id, COALESCE(u.name, ''), r.status, COALESCE(r.arr_id, 0), COALESCE(r.error, ''), r.created_at, r.updated_at`

func scanRequest(row interface{ Scan(...any) error }) (*Request, error) {
	var r Request
	if err := row.Scan(&r.ID, &r.MediaType, &r.TMDBID, &r.Title, &r.Year, &r.PosterPath, &r.UserID, &r.UserName, &r.Status, &r.ArrID, &r.Error, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) GetRequest(ctx context.Context, mediaType string, tmdbID int) (*Request, error) {
	r, err := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+requestCols+` FROM requests r LEFT JOIN users u ON u.id = r.user_id WHERE r.media_type = ? AND r.tmdb_id = ?`, mediaType, tmdbID))
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests (id, media_type, tmdb_id, title, year, poster_path, user_id, status, arr_id, error, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(media_type, tmdb_id) DO UPDATE SET status = excluded.status, arr_id = excluded.arr_id, error = excluded.error, updated_at = excluded.updated_at`,
		r.ID, r.MediaType, r.TMDBID, r.Title, nullInt(r.Year), nullStr(r.PosterPath), r.UserID, r.Status, nullInt(r.ArrID), nullStr(r.Error), r.CreatedAt, r.UpdatedAt)
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
