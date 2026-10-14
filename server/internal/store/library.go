package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roxy-media/roxy/server/internal/model"
)

func (s *Store) ListLibraries(ctx context.Context) ([]model.Library, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, root, anime_mode FROM libraries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Library
	for rows.Next() {
		var l model.Library
		var anime int
		if err := rows.Scan(&l.ID, &l.Name, &l.Kind, &l.Root, &anime); err != nil {
			return nil, err
		}
		l.AnimeMode = anime == 1
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetLibraryByRoot(ctx context.Context, root string) (*model.Library, error) {
	var l model.Library
	var anime int
	err := s.db.QueryRowContext(ctx, `SELECT id, name, kind, root, anime_mode FROM libraries WHERE root = ?`, root).
		Scan(&l.ID, &l.Name, &l.Kind, &l.Root, &anime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.AnimeMode = anime == 1
	return &l, nil
}

func (s *Store) CreateLibrary(ctx context.Context, l *model.Library) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO libraries (name, kind, root, anime_mode, created_at) VALUES (?, ?, ?, ?, ?)`,
		l.Name, l.Kind, l.Root, boolInt(l.AnimeMode), Now())
	if err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	return err
}
