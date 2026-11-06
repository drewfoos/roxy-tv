package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A login always keeps at least one profile, whatever the client asks: deleting down to
// one works, deleting that one is refused. Deleting the default hands it to the oldest left.
func TestDeleteProfileKeepsOne(t *testing.T) {
	s, _, ctx := openTest(t)
	u := &model.User{ID: "u1", Name: "drew"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateProfile(ctx, u.ID, "drew", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateProfile(ctx, u.ID, "coffee", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfile(ctx, a.ID); err != nil {
		t.Fatalf("deleting one of two: %v", err)
	}
	if err := s.DeleteProfile(ctx, b.ID); err == nil {
		t.Fatal("the last profile was deleted")
	}
	left, err := s.GetProfile(ctx, b.ID)
	if err != nil || left == nil {
		t.Fatalf("the last profile is gone: %v", err)
	}
	if !left.IsDefault {
		t.Error("the profile left is not the default")
	}
}

// Concurrent first requests for a new user must agree on one default profile; each used
// to see no profile and create its own.
func TestDefaultProfileIDConcurrentCreatesOne(t *testing.T) {
	for round := 0; round < 20; round++ {
		s, _, ctx := openTest(t)
		u := &model.User{ID: "u1", Name: "drew"}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		const n = 16
		ids := make([]string, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				ids[i], errs[i] = s.DefaultProfileID(ctx, u.ID)
			}(i)
		}
		close(start)
		wg.Wait()
		for i := range ids {
			if errs[i] != nil {
				t.Fatalf("round %d call %d: %v", round, i, errs[i])
			}
			if ids[i] != ids[0] {
				t.Fatalf("round %d: call %d got %s, call 0 got %s", round, i, ids[i], ids[0])
			}
		}
		ps, err := s.ListProfiles(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 1 || !ps[0].IsDefault {
			t.Fatalf("round %d: %d profiles created, want 1 default", round, len(ps))
		}
	}
}

// Two concurrent deletes on a two-profile login must leave one profile; each used to
// count two and both deletes went through.
func TestDeleteProfileConcurrentKeepsOne(t *testing.T) {
	for round := 0; round < 20; round++ {
		s, _, ctx := openTest(t)
		u := &model.User{ID: "u1", Name: "drew"}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		a, err := s.CreateProfile(ctx, u.ID, "drew", "", "")
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.CreateProfile(ctx, u.ID, "coffee", "", "")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, id := range []string{a.ID, b.ID} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				<-start
				_ = s.DeleteProfile(ctx, id)
			}(id)
		}
		close(start)
		wg.Wait()
		ps, err := s.ListProfiles(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 1 {
			t.Fatalf("round %d: %d profiles left, want 1", round, len(ps))
		}
		if !ps[0].IsDefault {
			t.Fatalf("round %d: the profile left is not the default", round)
		}
	}
}

// Migration 0028 repairs logins that already have several defaults: the oldest stays the
// default, the others are demoted but kept (they may hold watch state), and a login with
// no default gets its oldest profile.
func TestMigrationOneDefaultProfileRepairsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := s.DB()
	for _, q := range []string{
		`DROP INDEX IF EXISTS profiles_one_default`,
		`DELETE FROM schema_migrations WHERE version = '0028_one_default_profile.sql'`,
		`INSERT INTO users (id, name, password_hash, is_admin, is_disabled, created_at, role) VALUES ('u1','drew','x',0,0,'2026-01-01T00:00:00.000Z','user'), ('u2','coffee','x',0,0,'2026-01-01T00:00:00.000Z','user')`,
		`INSERT INTO profiles (id, user_id, name, is_default, created_at) VALUES
			('p3','u1','drew',1,'2026-01-03T00:00:00.000Z'),
			('p1','u1','drew',1,'2026-01-01T00:00:00.000Z'),
			('p2','u1','drew',1,'2026-01-02T00:00:00.000Z'),
			('q2','u2','b',0,'2026-01-02T00:00:00.000Z'),
			('q1','u2','a',0,'2026-01-01T00:00:00.000Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := map[string]bool{"p1": true, "p2": false, "p3": false, "q1": true, "q2": false}
	for id, def := range want {
		p, err := s.GetProfile(ctx, id)
		if err != nil || p == nil {
			t.Fatalf("profile %s: %v (deleted?)", id, err)
		}
		if p.IsDefault != def {
			t.Errorf("profile %s default = %v, want %v", id, p.IsDefault, def)
		}
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE profiles SET is_default = 1 WHERE id = 'p2'`); err == nil {
		t.Error("a second default profile was allowed")
	}
}
