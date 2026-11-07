// Package store is Roxy's SQLite persistence layer: schema migrations and typed queries.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) { return openUpTo(path, "") }

// openUpTo is Open applying only the migrations up to and including last ("" for all):
// tests build a database as an older version of Roxy left it, then upgrade it.
func openUpTo(path, last string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is happiest with one writer; the pool just serialises for us.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background(), last); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context, last string) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	applied := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if last != "" && name > last {
			break
		}
		if applied[name] {
			continue
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		// A migration that edits sqlite_master directly (writable_schema) must bump the
		// schema cookie, or connections keep using the schema they parsed before it.
		if strings.Contains(string(body), "writable_schema") {
			var v int
			if err := tx.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&v); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA schema_version = %d`, v+1)); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, name, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("applied migration", "name", name)
	}
	if last == "" || last >= "0034_extras.sql" {
		if err := s.ensureExtraType(ctx); err != nil {
			return err
		}
	}
	return nil
}

// reTypeCheck finds the items table's CHECK on type, however it is spaced or quoted.
var reTypeCheck = regexp.MustCompile(`(?is)(CHECK\s*\(\s*"?type"?\s+IN\s*\()([^)]*)(\)\s*\))`)

// ensureExtraType makes sure the items table accepts extras. Migration 0034 widens the
// type CHECK by replacing its exact text: on a table whose text differs at all (spacing,
// quoting, a database rebuilt by hand) that replacement does nothing, yet the migration
// is recorded as applied, and every extra was refused from then on, with no way back.
// The CHECK is found here by pattern and widened the same way; if it cannot be found,
// opening the database fails rather than running without extras.
func (s *Store) ensureExtraType(ctx context.Context) error {
	var sqlText string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'items'`).Scan(&sqlText); err != nil {
		return err
	}
	m := reTypeCheck.FindStringSubmatchIndex(sqlText)
	if m == nil {
		return fmt.Errorf("the items table has no type CHECK that extras can be added to; its definition: %s", sqlText)
	}
	if strings.Contains(strings.ToLower(sqlText[m[4]:m[5]]), "'extra'") {
		return nil
	}
	fixed := sqlText[:m[5]] + ", 'extra'" + sqlText[m[5]:]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var v int
	if err := tx.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&v); err != nil {
		return err
	}
	for _, q := range []string{`PRAGMA writable_schema = ON`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sqlite_master SET sql = ? WHERE type = 'table' AND name = 'items'`, fixed); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA writable_schema = OFF`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA schema_version = %d`, v+1)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	var ic string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		return fmt.Errorf("after letting the items table accept extras: integrity_check %q %v", ic, err)
	}
	slog.Warn("the items table did not accept extras (migration 0034 found a different table text): fixed")
	return nil
}

// Now returns the canonical timestamp format stored in the database (RFC3339, UTC, ms).
func Now() string { return time.Now().UTC().Format(TimeFormat) }

const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

func ParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(TimeFormat, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt(i int) sql.NullInt64     { return sql.NullInt64{Int64: int64(i), Valid: i != 0} }
func nullInt64(i int64) sql.NullInt64 { return sql.NullInt64{Int64: i, Valid: i != 0} }
func nullFloat(f float64) sql.NullFloat64 {
	return sql.NullFloat64{Float64: f, Valid: f != 0}
}
func ptrInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
func ptrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
func ptrStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
func intPtrNull(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}
func int64PtrNull(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}
func strPtrNull(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}
