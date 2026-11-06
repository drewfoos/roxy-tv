package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

func accountFixture(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	for _, u := range []*model.User{
		{ID: "admin", Name: "Drew", Role: model.RoleAdmin, IsAdmin: true, PasswordHash: "old-admin"},
		{ID: "kid", Name: "Coffee", Role: model.RoleRequester, PasswordHash: "old-kid"},
	} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range []string{"tv", "phone"} {
		if err := s.CreateDevice(ctx, &model.Device{ID: d, UserID: "kid", TokenHash: "h" + string(rune('0'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx
}

func hashOf(t *testing.T, s *Store, ctx context.Context, id string) string {
	t.Helper()
	u, err := s.GetUser(ctx, id)
	if err != nil || u == nil {
		t.Fatal(err)
	}
	return u.PasswordHash
}

func deviceCount(t *testing.T, s *Store, ctx context.Context, id string) int {
	t.Helper()
	ds, err := s.ListDevices(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return len(ds)
}

// An admin's reset is all or nothing: the rename, the new password and signing out.
func TestSaveUserChangesIsAtomic(t *testing.T) {
	s, ctx := accountFixture(t)
	kid, _ := s.GetUser(ctx, "kid")

	// The rename clashes (names are unique, any case): nothing may change, not even the password.
	kid.Name = "drew"
	if err := s.SaveUserChanges(ctx, kid, "new-kid", true); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename clash: %v", err)
	}
	if hashOf(t, s, ctx, "kid") != "old-kid" || deviceCount(t, s, ctx, "kid") != 2 {
		t.Fatal("a failed reset changed the password or signed devices out")
	}

	// The sign-out fails: the password must not change either.
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER no_signout BEFORE DELETE ON devices BEGIN SELECT RAISE(ABORT, 'disk I/O error'); END`); err != nil {
		t.Fatal(err)
	}
	kid.Name = "Coffee"
	if err := s.SaveUserChanges(ctx, kid, "new-kid", true); err == nil {
		t.Fatal("a failed sign-out was reported as success")
	}
	if hashOf(t, s, ctx, "kid") != "old-kid" || deviceCount(t, s, ctx, "kid") != 2 {
		t.Fatal("password changed although signing out failed")
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER no_signout`); err != nil {
		t.Fatal(err)
	}

	// Everything works: all of it applies.
	kid.Name, kid.IsDisabled = "Coffee Bean", false
	if err := s.SaveUserChanges(ctx, kid, "new-kid", true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetUser(ctx, "kid")
	if got.Name != "Coffee Bean" || got.PasswordHash != "new-kid" || deviceCount(t, s, ctx, "kid") != 0 {
		t.Fatalf("reset not applied: %+v, %d devices", got, deviceCount(t, s, ctx, "kid"))
	}
}

// The only active admin cannot be demoted or disabled, checked inside the transaction.
func TestSaveUserChangesKeepsAnAdmin(t *testing.T) {
	s, ctx := accountFixture(t)
	admin, _ := s.GetUser(ctx, "admin")
	admin.Role = model.RoleRequester
	if err := s.SaveUserChanges(ctx, admin, "", false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the last admin: %v", err)
	}
	admin.Role, admin.IsDisabled = model.RoleAdmin, true
	if err := s.SaveUserChanges(ctx, admin, "", false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disabling the last admin: %v", err)
	}
	if got, _ := s.GetUser(ctx, "admin"); got.Role != model.RoleAdmin || got.IsDisabled {
		t.Fatalf("last admin changed: %+v", got)
	}
}

// A user's own password change: the new hash and signing the other devices out together.
func TestChangePasswordIsAtomic(t *testing.T) {
	s, ctx := accountFixture(t)
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER no_signout BEFORE DELETE ON devices BEGIN SELECT RAISE(ABORT, 'disk I/O error'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, "kid", "new-kid", "tv"); err == nil {
		t.Fatal("a failed sign-out was reported as success")
	}
	if hashOf(t, s, ctx, "kid") != "old-kid" {
		t.Fatal("password changed although signing out failed")
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER no_signout`); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, "kid", "new-kid", "tv"); err != nil {
		t.Fatal(err)
	}
	ds, _ := s.ListDevices(ctx, "kid")
	if hashOf(t, s, ctx, "kid") != "new-kid" || len(ds) != 1 || ds[0].ID != "tv" {
		t.Fatalf("change not applied: devices %+v", ds)
	}
}
