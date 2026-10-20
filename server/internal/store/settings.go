package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// GetSetting decodes the JSON document stored under key into out; ok is false when none is saved.
func (s *Store) GetSetting(ctx context.Context, key string, out any) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), out)
}

// SetSetting stores v as JSON under key.
func (s *Store) SetSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, string(b), Now())
	return err
}

// ServerID is this installation's stable random id, made the first time it is asked
// for. Clients use it to tell servers apart (and, later, to pair a TV by QR code).
func (s *Store) ServerID(ctx context.Context) (string, error) {
	var id string
	ok, err := s.GetSetting(ctx, "server_id", &id)
	if err != nil {
		return "", err
	}
	if ok && id != "" {
		return id, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id = hex.EncodeToString(b)
	return id, s.SetSetting(ctx, "server_id", id)
}

// ServerName is what clients show for this server: the "server_name" setting, else
// "Roxy on <host>".
func (s *Store) ServerName(ctx context.Context) string {
	var name string
	if ok, err := s.GetSetting(ctx, "server_name", &name); err == nil && ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return "Roxy on " + h
	}
	return "Roxy Server"
}
