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
// user) if none exists yet, so a user is never without a scope for watch state.
func (s *Store) DefaultProfileID(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM profiles WHERE user_id = ? ORDER BY is_default DESC, created_at ASC LIMIT 1`, userID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return "", err
	}
	p, err := s.CreateProfile(ctx, userID, name, "", "")
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE profiles SET is_default = 1 WHERE id = ?`, p.ID)
	return p.ID, err
}

func (s *Store) CreateProfile(ctx context.Context, userID, name, avatarURL, avatarName string) (*model.Profile, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= model.MaxProfilesPerUser {
		return nil, fmt.Errorf("at most %d profiles per account", model.MaxProfilesPerUser)
	}
	p := &model.Profile{ID: uuid.NewString(), UserID: userID, Name: name, AvatarURL: avatarURL, AvatarName: avatarName, IsDefault: n == 0, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO profiles (id, user_id, name, avatar_url, avatar_name, is_default, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
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

func (s *Store) SetProfileAvatarPath(ctx context.Context, id, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE profiles SET avatar_path = ? WHERE id = ?`, path, id)
	return err
}

// DeleteProfile removes a profile and its watch state. Deleting the default promotes the
// oldest remaining profile; the last profile cannot be deleted.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	p, err := s.GetProfile(ctx, id)
	if err != nil || p == nil {
		return err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE user_id = ?`, p.UserID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("the last profile cannot be deleted")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id); err != nil {
		return err
	}
	if p.IsDefault {
		_, err = s.db.ExecContext(ctx, `UPDATE profiles SET is_default = 1 WHERE id = (SELECT id FROM profiles WHERE user_id = ? ORDER BY created_at ASC LIMIT 1)`, p.UserID)
	}
	return err
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
