package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// ItemRelation is a provider-linked title (movie, OVA, side story...) of an item.
type ItemRelation struct {
	Provider   string
	ProviderID string
	Relation   string
	Title      string
	Format     string
	Year       int
	PosterURL  string
	Episodes   int
}

// ReplaceRelations rewrites an item's relation list.
func (s *Store) ReplaceRelations(ctx context.Context, itemID string, rels []ItemRelation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_relations WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, r := range rels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO item_relations (item_id, provider, provider_id, relation, title, format, year, poster_url, episodes) VALUES (?,?,?,?,?,?,?,?,?)`,
			itemID, r.Provider, r.ProviderID, r.Relation, r.Title, nullStr(r.Format), nullInt(r.Year), nullStr(r.PosterURL), nullInt(r.Episodes)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RelationsFor lists an item's relations.
func (s *Store) RelationsFor(ctx context.Context, itemID string) ([]ItemRelation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, provider_id, relation, title, format, year, poster_url, episodes FROM item_relations WHERE item_id = ? ORDER BY year, title`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRelation
	for rows.Next() {
		var r ItemRelation
		var format, poster sql.NullString
		var year, eps sql.NullInt64
		if err := rows.Scan(&r.Provider, &r.ProviderID, &r.Relation, &r.Title, &format, &year, &poster, &eps); err != nil {
			return nil, err
		}
		r.Format, r.PosterURL, r.Year, r.Episodes = format.String, poster.String, int(year.Int64), int(eps.Int64)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ItemsByProviderIDs resolves (provider, id) pairs to top-level library items (series or
// movies) in one query. Key of the result: provider + ":" + id.
func (s *Store) ItemsByProviderIDs(ctx context.Context, provider string, ids []string) (map[string]model.Item, error) {
	out := map[string]model.Item{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{provider}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsFor("i")+`, p.value FROM item_provider_ids p JOIN items i ON i.id = p.item_id
		WHERE p.provider = ? AND p.value IN (`+ph+`) AND i.type IN ('series', 'movie')`, args...)
	if err != nil {
		return nil, err
	}
	var items []model.Item
	var keys []string
	for rows.Next() {
		var val string
		it, err := scanItem(rows, &val)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, *it)
		keys = append(keys, provider+":"+val)
	}
	rows.Close()
	if err := s.fillItemExtras(ctx, items); err != nil {
		return nil, err
	}
	for i := range items {
		out[keys[i]] = items[i]
	}
	return out, nil
}

// ParentsByRelation finds items that list (provider, id) as a relation: the reverse
// link a movie page uses to show "part of <series>".
func (s *Store) ParentsByRelation(ctx context.Context, provider, providerID string) ([]model.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsFor("i")+` FROM item_relations r JOIN items i ON i.id = r.item_id
		WHERE r.provider = ? AND r.provider_id = ? ORDER BY i.type, i.title`, provider, providerID)
	if err != nil {
		return nil, err
	}
	var items []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, *it)
	}
	rows.Close()
	return items, s.fillItemExtras(ctx, items)
}
