package store

import (
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Device Management lists every device a login signed in with, and one login cannot sign
// another login's device out.
func TestListAndDeleteUserDevices(t *testing.T) {
	s, _, ctx := openTest(t)
	for _, u := range []*model.User{{ID: "u1", Name: "drew"}, {ID: "u2", Name: "guest"}} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []*model.Device{
		{ID: "d1", UserID: "u1", Name: "Phone", TokenHash: "h1"},
		{ID: "d2", UserID: "u1", Name: "TV", TokenHash: "h2"},
		{ID: "d3", UserID: "u2", Name: "Other", TokenHash: "h3"},
	} {
		if err := s.CreateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListDevices(ctx, "u1")
	if err != nil || len(got) != 2 {
		t.Fatalf("u1 devices %v err %v, want 2", got, err)
	}
	if ok, err := s.DeleteUserDevice(ctx, "u1", "d3"); ok || err != nil {
		t.Fatalf("deleted another login's device: ok %v err %v", ok, err)
	}
	if ok, err := s.DeleteUserDevice(ctx, "u1", "d2"); !ok || err != nil {
		t.Fatalf("own device: ok %v err %v", ok, err)
	}
	if got, _ := s.ListDevices(ctx, "u1"); len(got) != 1 || got[0].ID != "d1" {
		t.Fatalf("after delete: %v", got)
	}
	if d, _ := s.GetDeviceByTokenHash(ctx, "h2"); d != nil {
		t.Fatal("signed-out device's token still resolves")
	}
}
