package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	now := Now()
	if u.Role == "" {
		u.Role = model.RoleRequester
		if u.IsAdmin {
			u.Role = model.RoleAdmin
		}
	}
	u.IsAdmin = u.Role == model.RoleAdmin
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, name, password_hash, is_admin, is_disabled, created_at, role) VALUES (?,?,?,?,?,?,?)`,
		u.ID, u.Name, nullStr(u.PasswordHash), boolInt(u.IsAdmin), boolInt(u.IsDisabled), now, u.Role)
	if err == nil {
		u.CreatedAt = ParseTime(now)
	}
	return err
}

const userCols = `id, name, password_hash, is_admin, is_disabled, created_at, role`

func scanUser(sc interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	var hash sql.NullString
	var admin, disabled int
	var created string
	if err := sc.Scan(&u.ID, &u.Name, &hash, &admin, &disabled, &created, &u.Role); err != nil {
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (id, user_id, name, client, client_version, device_id, token_hash, last_seen, created_at, last_ip) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, nullStr(d.Name), nullStr(d.Client), nullStr(d.ClientVersion), nullStr(d.DeviceID), d.TokenHash, now, now, nullStr(d.LastIP))
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

// ListDevices: every device signed in with the user's login, most recently seen first.
func (s *Store) ListDevices(ctx context.Context, userID string) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, name, client, client_version, device_id, last_seen, created_at, last_ip FROM devices WHERE user_id = ? ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Device{}
	for rows.Next() {
		var d model.Device
		var name, client, ver, devID, seen, ip sql.NullString
		var created string
		if err := rows.Scan(&d.ID, &d.UserID, &name, &client, &ver, &devID, &seen, &created, &ip); err != nil {
			return nil, err
		}
		d.Name, d.Client, d.ClientVersion, d.DeviceID, d.LastIP = name.String, client.String, ver.String, devID.String, ip.String
		d.LastSeen, d.CreatedAt = ParseTime(seen.String), ParseTime(created)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeviceExists: the device is still signed in.
func (s *Store) DeviceExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// DeleteUserDevice signs one of the user's devices out; false when it isn't theirs.
func (s *Store) DeleteUserDevice(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TouchDevice marks a device seen now, from ip when known (empty keeps the last one).
func (s *Store) TouchDevice(ctx context.Context, id, ip string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE devices SET last_seen = ?, last_ip = COALESCE(NULLIF(?, ''), last_ip) WHERE id = ?`, Now(), ip, id)
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	return err
}

// DeleteDevicesExcept signs a user out everywhere but keepID (after a password change).
func (s *Store) DeleteDevicesExcept(ctx context.Context, userID, keepID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND id <> ?`, userID, keepID)
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

var (
	// ErrLastAdmin: the change would leave no active admin.
	ErrLastAdmin = errors.New("this is the only admin; make someone else admin first")
	// ErrNameTaken: another account already has that name (names are unique, any case).
	ErrNameTaken = errors.New("another account already has that name")
)

// ChangePassword sets a new password hash and signs the account out of every device but
// keepDevice, in one transaction: a password change used to commit the hash first, so a
// failed sign-out reported an error while the password had already changed and the other
// devices stayed signed in.
func (s *Store) ChangePassword(ctx context.Context, userID, hash, keepDevice string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND id <> ?`, userID, keepDevice); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveUserChanges applies an admin's changes to an account in one transaction: name,
// level and disabled flag, a new password hash (when not empty), and signing the account
// out of every device (signOut). Either all of it happens or none does. It used to be
// separate statements, so a reset whose rename then failed (names are unique) changed the
// password but kept the old sessions, while the admin saw an error; and a failed sign-out
// was ignored, so a reset meant to lock someone out could leave them signed in.
//
// The last-admin rule is checked inside the same transaction (a check before it could
// pass for two admins demoting each other at once).
func (s *Store) SaveUserChanges(ctx context.Context, u *model.User, passwordHash string, signOut bool) error {
	u.IsAdmin = u.Role == model.RoleAdmin
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !u.IsAdmin || u.IsDisabled {
		var wasActiveAdmin, others int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ? AND role = 'admin' AND is_disabled = 0`, u.ID).Scan(&wasActiveAdmin); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id <> ? AND role = 'admin' AND is_disabled = 0`, u.ID).Scan(&others); err != nil {
			return err
		}
		if wasActiveAdmin == 1 && others == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET name = ?, role = ?, is_admin = ?, is_disabled = ? WHERE id = ?`,
		u.Name, u.Role, boolInt(u.IsAdmin), boolInt(u.IsDisabled), u.ID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.name") {
			return ErrNameTaken
		}
		return err
	}
	if passwordHash != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, u.ID); err != nil {
			return err
		}
	}
	if signOut {
		if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ?`, u.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateUser saves a user's name, level and disabled flag (is_admin follows the level).
func (s *Store) UpdateUser(ctx context.Context, u *model.User) error {
	u.IsAdmin = u.Role == model.RoleAdmin
	_, err := s.db.ExecContext(ctx, `UPDATE users SET name = ?, role = ?, is_admin = ?, is_disabled = ? WHERE id = ?`,
		u.Name, u.Role, boolInt(u.IsAdmin), boolInt(u.IsDisabled), u.ID)
	return err
}

// DeleteUser removes an account; its devices, profiles and watch history go with it
// (cascade). Its requests would too, so they move to heir (the admin deleting it) first,
// still showing the name of whoever asked.
func (s *Store) DeleteUser(ctx context.Context, id, heir string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE requests SET requested_by = COALESCE(NULLIF(requested_by, ''), (SELECT name FROM users WHERE id = ?)), user_id = ? WHERE user_id = ?`, id, heir, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CountActiveAdmins counts admins who can sign in (the server must always keep one).
func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_disabled = 0`).Scan(&n)
	return n, err
}
