package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Profiles (migration 0007): several viewers on one login. Watch state is keyed by the
// profile id, which the API calls the "scope".

func (s *Store) ListProfiles(ctx context.Context, userID string) ([]model.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, name, avatar_url, avatar_path, avatar_name, is_default, created_at
		FROM profiles WHERE user_id = ? ORDER BY is_default DESC, created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetProfile(ctx context.Context, id string) (*model.Profile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id, name, avatar_url, avatar_path, avatar_name, is_default, created_at FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// DefaultProfileID returns the user's default profile, creating one (named after the
// user) if none exists yet, so a user is never without a scope for watch state. The
// check and the insert share one transaction: concurrent first requests for a new user
// used to each see no profile and each create a default. The partial unique index from
// migration 0028 backs this up if anything else ever races it.
func (s *Store) DefaultProfileID(ctx context.Context, userID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM profiles WHERE user_id = ? ORDER BY is_default DESC, created_at ASC LIMIT 1`, userID).Scan(&id)
	if err == nil {
		return id, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return "", err
	}
	p, err := insertProfile(ctx, tx, userID, name, "", "", true)
	if err != nil {
		return "", err
	}
	return p.ID, tx.Commit()
}

// CreateProfile adds a profile to a login. The count and the insert share a transaction
// so two concurrent creates can neither pass the limit nor both become the default.
func (s *Store) CreateProfile(ctx context.Context, userID, name, avatarURL, avatarName string) (*model.Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= model.MaxProfilesPerUser {
		return nil, fmt.Errorf("at most %d profiles per account", model.MaxProfilesPerUser)
	}
	p, err := insertProfile(ctx, tx, userID, name, avatarURL, avatarName, n == 0)
	if err != nil {
		return nil, err
	}
	return p, tx.Commit()
}

func insertProfile(ctx context.Context, tx *sql.Tx, userID, name, avatarURL, avatarName string, isDefault bool) (*model.Profile, error) {
	p := &model.Profile{ID: uuid.NewString(), UserID: userID, Name: name, AvatarURL: avatarURL, AvatarName: avatarName, IsDefault: isDefault, CreatedAt: time.Now().UTC()}
	_, err := tx.ExecContext(ctx, `INSERT INTO profiles (id, user_id, name, avatar_url, avatar_name, is_default, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.Name, nullStr(p.AvatarURL), nullStr(p.AvatarName), boolInt(p.IsDefault), p.CreatedAt.Format(TimeFormat))
	return p, err
}

// UpdateProfile changes the name and/or avatar source; an empty avatarURL keeps the
// current one, "-" clears it.
func (s *Store) UpdateProfile(ctx context.Context, id, name, avatarURL, avatarName string) error {
	if name != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE profiles SET name = ? WHERE id = ?`, name, id); err != nil {
			return err
		}
	}
	switch avatarURL {
	case "":
	case "-":
		_, err := s.db.ExecContext(ctx, `UPDATE profiles SET avatar_url = NULL, avatar_path = NULL, avatar_name = NULL WHERE id = ?`, id)
		return err
	default:
		_, err := s.db.ExecContext(ctx, `UPDATE profiles SET avatar_url = ?, avatar_name = ?, avatar_path = NULL WHERE id = ?`, avatarURL, nullStr(avatarName), id)
		return err
	}
	return nil
}

// SetProfileAvatarPath records the cached file for the profile's picture, only if url is
// still the picture the profile has chosen (checked in the same statement). False when it
// is not: a newer choice came in while this one was downloading.
func (s *Store) SetProfileAvatarPath(ctx context.Context, id, url, path string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE profiles SET avatar_path = ? WHERE id = ? AND avatar_url = ?`, path, id, url)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteProfile removes a profile and its watch state. Deleting the default promotes the
// oldest remaining profile; the last profile cannot be deleted. The count and the delete
// share a transaction: two concurrent deletes on a two-profile login could otherwise each
// count two and delete both.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := scanProfile(tx.QueryRowContext(ctx, `SELECT id, user_id, name, avatar_url, avatar_path, avatar_name, is_default, created_at FROM profiles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE user_id = ?`, p.UserID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("the last profile cannot be deleted")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id); err != nil {
		return err
	}
	if p.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE profiles SET is_default = 1 WHERE id = (SELECT id FROM profiles WHERE user_id = ? ORDER BY created_at ASC LIMIT 1)`, p.UserID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanProfile(r rowScanner) (*model.Profile, error) {
	var p model.Profile
	var url, path, aname sql.NullString
	var def int
	var created string
	if err := r.Scan(&p.ID, &p.UserID, &p.Name, &url, &path, &aname, &def, &created); err != nil {
		return nil, err
	}
	p.AvatarURL, p.AvatarPath, p.AvatarName = url.String, path.String, aname.String
	p.IsDefault = def == 1
	p.CreatedAt = ParseTime(created)
	return &p, nil
}
