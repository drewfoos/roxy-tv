package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

// TestRestoreDrill opens a copy of a backup with this build, as a restore would: every
// pending migration is applied, and nothing is lost (rows of the tables that hold the
// library and people's data before and after), SQLite's checks pass, and CheckIntegrity
// finds nothing. It changes the file it is given, so give it a copy:
//
//	cp roxy.db.bak-... /tmp/drill.db
//	ROXY_RESTORE_COPY=/tmp/drill.db go test ./internal/store -run RestoreDrill -v
func TestRestoreDrill(t *testing.T) {
	path := os.Getenv("ROXY_RESTORE_COPY")
	if path == "" {
		t.Skip("set ROXY_RESTORE_COPY to a copy of a backup")
	}
	tables := []string{"libraries", "items", "media_sources", "media_parts", "media_streams", "user_item_data", "users", "profiles", "item_provider_ids", "item_images"}
	count := func(db *sql.DB) map[string]int {
		out := map[string]int{}
		for _, tb := range tables {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tb).Scan(&n); err != nil {
				t.Fatalf("%s: %v", tb, err)
			}
			out[tb] = n
		}
		return out
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	before := count(raw)
	var applied int
	_ = raw.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied)
	raw.Close()

	s, err := Open(path) // applies the pending migrations
	if err != nil {
		t.Fatalf("opening the restored database: %v", err)
	}
	defer s.Close()
	after := count(s.db)
	var now int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&now)
	t.Logf("migrations: %d before, %d after", applied, now)
	for _, tb := range tables {
		t.Logf("%-18s %6d -> %6d", tb, before[tb], after[tb])
		if after[tb] < before[tb] {
			t.Errorf("%s lost rows: %d -> %d", tb, before[tb], after[tb])
		}
	}
	probs, err := s.CheckIntegrity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		t.Errorf("after restore: %s", p)
	}
}
