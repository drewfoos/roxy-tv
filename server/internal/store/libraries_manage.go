package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roxy-media/roxy/server/internal/model"
)

// LibraryInfo is a library with how much is in it, for the Media folders screen.
type LibraryInfo struct {
	model.Library
	Series int `json:"series"`
	Movies int `json:"movies"`
	Files  int `json:"files"`
}

// LibraryInfos lists every library with its title and file counts.
func (s *Store) LibraryInfos(ctx context.Context) ([]LibraryInfo, error) {
	libs, err := s.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LibraryInfo, 0, len(libs))
	for _, l := range libs {
		li := LibraryInfo{Library: l}
		_ = s.db.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(type = 'series'), 0), COALESCE(SUM(type = 'movie' AND is_virtual = 0), 0)
			FROM items WHERE library_id = ?`, l.ID).Scan(&li.Series, &li.Movies)
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_parts mp JOIN media_sources ms ON ms.id = mp.source_id
			JOIN items i ON i.id = ms.item_id WHERE i.library_id = ?`, l.ID).Scan(&li.Files)
		out = append(out, li)
	}
	return out, nil
}

// GetLibrary returns one library, or nil.
func (s *Store) GetLibrary(ctx context.Context, id int64) (*model.Library, error) {
	var l model.Library
	var anime int
	err := s.db.QueryRowContext(ctx, `SELECT id, name, kind, root, anime_mode FROM libraries WHERE id = ?`, id).
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

// UpdateLibrary changes a library's name, type and anime mode (not its folder).
func (s *Store) UpdateLibrary(ctx context.Context, l *model.Library) error {
	_, err := s.db.ExecContext(ctx, `UPDATE libraries SET name = ?, kind = ?, anime_mode = ? WHERE id = ?`,
		l.Name, l.Kind, boolInt(l.AnimeMode), l.ID)
	return err
}

// LibraryRootIdentity is the library folder's identity as the last scan that found files
// in it recorded it ('' if none has).
func (s *Store) LibraryRootIdentity(ctx context.Context, id int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT root_identity FROM libraries WHERE id = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetLibraryRootIdentity records the library folder's identity.
func (s *Store) SetLibraryRootIdentity(ctx context.Context, id int64, v string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE libraries SET root_identity = ? WHERE id = ?`, v, id)
	return err
}

// DeleteLibrary forgets a library and everything Roxy knew about its titles (the rows
// cascade). Files on disk are not touched; watch state is keyed by GUID and comes back
// if the folder is added again.
func (s *Store) DeleteLibrary(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id)
	return err
}

// RemovedRoots are folders the admin removed in the app; ROXY_MEDIA_ROOTS does not bring
// them back on the next start.
func (s *Store) RemovedRoots(ctx context.Context) (map[string]bool, error) {
	var list []string
	if _, err := s.GetSetting(ctx, "removed_library_roots", &list); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range list {
		out[r] = true
	}
	return out, nil
}

// SetRootRemoved records (or clears) a folder as removed by the admin.
func (s *Store) SetRootRemoved(ctx context.Context, root string, removed bool) error {
	cur, err := s.RemovedRoots(ctx)
	if err != nil {
		return err
	}
	if removed {
		cur[root] = true
	} else {
		delete(cur, root)
	}
	list := make([]string, 0, len(cur))
	for r := range cur {
		list = append(list, r)
	}
	return s.SetSetting(ctx, "removed_library_roots", list)
}
