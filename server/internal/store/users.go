package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roxy-media/roxy/server/internal/model"
)

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	now := Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, name, password_hash, is_admin, is_disabled, created_at) VALUES (?,?,?,?,?,?)`,
		u.ID, u.Name, nullStr(u.PasswordHash), boolInt(u.IsAdmin), boolInt(u.IsDisabled), now)
	if err == nil {
		u.CreatedAt = ParseTime(now)
	}
	return err
}

const userCols = `id, name, password_hash, is_admin, is_disabled, created_at`

func scanUser(sc interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	var hash sql.NullString
	var admin, disabled int
	var created string
	if err := sc.Scan(&u.ID, &u.Name, &hash, &admin, &disabled, &created); err != nil {
		return nil, err
	}
	u.PasswordHash = hash.String
	u.IsAdmin, u.IsDisabled = admin == 1, disabled == 1
	u.CreatedAt = ParseTime(created)
	return &u, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*model.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *Store) GetUserByName(ctx context.Context, name string) (*model.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE name = ? COLLATE NOCASE`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ---- devices (sessions) ----

func (s *Store) CreateDevice(ctx context.Context, d *model.Device) error {
	now := Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (id, user_id, name, client, client_version, device_id, token_hash, last_seen, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, nullStr(d.Name), nullStr(d.Client), nullStr(d.ClientVersion), nullStr(d.DeviceID), d.TokenHash, now, now)
	if err == nil {
		d.CreatedAt, d.LastSeen = ParseTime(now), ParseTime(now)
	}
	return err
}

func (s *Store) GetDeviceByTokenHash(ctx context.Context, hash string) (*model.Device, error) {
	var d model.Device
	var name, client, ver, devID, seen sql.NullString
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, name, client, client_version, device_id, token_hash, last_seen, created_at FROM devices WHERE token_hash = ?`, hash).
		Scan(&d.ID, &d.UserID, &name, &client, &ver, &devID, &d.TokenHash, &seen, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Name, d.Client, d.ClientVersion, d.DeviceID = name.String, client.String, ver.String, devID.String
	d.LastSeen, d.CreatedAt = ParseTime(seen.String), ParseTime(created)
	return &d, nil
}

func (s *Store) TouchDevice(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, Now(), id)
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	return err
}

// ---- API keys ----

func (s *Store) CreateAPIKey(ctx context.Context, k *model.APIKey) error {
	now := Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys (id, name, token_hash, created_at) VALUES (?,?,?,?)`, k.ID, k.Name, k.TokenHash, now)
	if err == nil {
		k.CreatedAt = ParseTime(now)
	}
	return err
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var k model.APIKey
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, token_hash, created_at FROM api_keys WHERE token_hash = ?`, hash).Scan(&k.ID, &k.Name, &k.TokenHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.CreatedAt = ParseTime(created)
	return &k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, token_hash, created_at FROM api_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.APIKey
	for rows.Next() {
		var k model.APIKey
		var created string
		if err := rows.Scan(&k.ID, &k.Name, &k.TokenHash, &created); err != nil {
			return nil, err
		}
		k.CreatedAt = ParseTime(created)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKeyByHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE token_hash = ?`, hash)
	return err
}

// AcceptedTerms is the terms version this user accepted ("" = never).
func (s *Store) AcceptedTerms(ctx context.Context, userID string) (string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT accepted_terms FROM users WHERE id = ?`, userID).Scan(&v)
	return v.String, err
}

func (s *Store) SetAcceptedTerms(ctx context.Context, userID, version string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET accepted_terms = ? WHERE id = ?`, version, userID)
	return err
}

// SetPasswordHash replaces a user's password hash (a cost upgrade, or a new password).
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	return err
}
