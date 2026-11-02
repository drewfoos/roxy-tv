package store

import (
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
