package store

import (
	"context"
	"strings"
)

// WebhookEvent is one received arr notification and its outcome.
type WebhookEvent struct {
	ID        int64  `json:"id"`
	At        string `json:"at"`
	Source    string `json:"source"`
	EventType string `json:"eventType"`
	Title     string `json:"title"`
	Path      string `json:"path,omitempty"`
	Result    string `json:"result"`
}

func (s *Store) AddWebhookEvent(ctx context.Context, e WebhookEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhook_events (at, source, event_type, title, path, result) VALUES (?,?,?,?,?,?)`,
		Now(), e.Source, e.EventType, e.Title, e.Path, e.Result)
	if err != nil {
		return err
	}
	// Keep the table small: the last 500 are plenty for debugging a deploy.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM webhook_events WHERE id NOT IN (SELECT id FROM webhook_events ORDER BY id DESC LIMIT 500)`)
	return nil
}

func (s *Store) ListWebhookEvents(ctx context.Context, limit int) ([]WebhookEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, source, event_type, title, path, result FROM webhook_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebhookEvent{}
	for rows.Next() {
		var e WebhookEvent
		if err := rows.Scan(&e.ID, &e.At, &e.Source, &e.EventType, &e.Title, &e.Path, &e.Result); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteSourcesUnder removes every media source whose first part lives under dir
// (a series or movie folder the arr app deleted). Returns how many went.
func (s *Store) DeleteSourcesUnder(ctx context.Context, dir string) (int, error) {
	dir = strings.TrimRight(dir, `\/`)
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT source_id FROM media_parts WHERE path = ? OR path LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\'`,
		dir, likeEscape(dir)+`/%`, likeEscape(dir)+`\\%`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.DeleteSource(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// CountSourcesUnder says whether any media part lives under dir (for resolving the
// arr apps' folder paths against what the library knows, since a deleted folder is
// gone from disk by the time the notification arrives).
func (s *Store) CountSourcesUnder(ctx context.Context, dir string) (int, error) {
	dir = strings.TrimRight(dir, `\/`)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT source_id) FROM media_parts WHERE path = ? OR path LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\'`,
		dir, likeEscape(dir)+`/%`, likeEscape(dir)+`\\%`).Scan(&n)
	return n, err
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// DeleteAPIKeyByID removes an API key (the admin's Webhooks page).
func (s *Store) DeleteAPIKeyByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	return err
}
