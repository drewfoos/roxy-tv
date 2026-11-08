package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func credFixture(t *testing.T) (*Service, *store.Store, *model.User, *model.Device, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st)
	u := &model.User{ID: "u1", Name: "alice", Role: model.RoleBasic}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	d, tok, err := s.IssueToken(context.Background(), u, ClientInfo{Client: "test", DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	return s, st, u, d, tok
}

// signMedia signs a media token payload with the server's key, as NewMediaToken does,
// but with any expiry (and so any shape) a test needs.
func signMedia(t *testing.T, s *Service, payload string) string {
	t.Helper()
	key, err := s.mediaSecret(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(mac.Sum(nil))
}

// A media token works for its source until it expires, and stops at once when it is
// tampered with, when it has expired, when the device it names signs out, and when its
// user is disabled or deleted.
func TestMediaTokenLifetime(t *testing.T) {
	ctx := context.Background()
	s, st, u, d, _ := credFixture(t)
	tok, _, err := s.NewMediaToken(ctx, "src1", u.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if src, uid, err := s.verifyMediaToken(ctx, tok); err != nil || src != "src1" || uid != u.ID {
		t.Fatalf("a fresh token: %q %q %v", src, uid, err)
	}
	// Tampered: another source in the payload, the signature kept.
	dot := strings.LastIndex(tok, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(tok[:dot])
	forged := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), "src1", "src2", 1))) + tok[dot:]
	if _, _, err := s.verifyMediaToken(ctx, forged); err == nil {
		t.Error("a token whose source was changed was accepted")
	}
	// Expired: signed correctly, an hour past its expiry.
	old := signMedia(t, s, "src1."+u.ID+"."+d.ID+"."+strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))
	if _, _, err := s.verifyMediaToken(ctx, old); err == nil {
		t.Error("an expired media token was accepted")
	}
	// The old format, without a device (correctly signed, not expired), is refused: it
	// could not be ended by signing out. So is one naming no device.
	future := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	if _, _, err := s.verifyMediaToken(ctx, signMedia(t, s, "src1."+u.ID+"."+future)); err == nil {
		t.Error("an old-format media token (no device) was accepted")
	}
	if _, _, err := s.verifyMediaToken(ctx, signMedia(t, s, "src1."+u.ID+".."+future)); err == nil {
		t.Error("a media token naming no device was accepted")
	}
	if _, _, err := s.NewMediaToken(ctx, "src1", u.ID, ""); err == nil {
		t.Error("a media token was issued with no device")
	}
	// The device signs out: its tokens end.
	if err := st.DeleteDevice(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.verifyMediaToken(ctx, tok); err == nil {
		t.Error("a media token outlived its device's sign-out")
	}
}

// Device tokens: a disabled user's stop working, and so does one unused for longer than
// DeviceIdleExpiry (the device is removed); one deleted (signed out) is unknown.
func TestDeviceTokenEnds(t *testing.T) {
	ctx := context.Background()
	s, st, u, d, tok := credFixture(t)
	if p, err := s.Resolve(ctx, tok); err != nil || p == nil || p.UserID != u.ID {
		t.Fatalf("a fresh token: %+v %v", p, err)
	}
	u.IsDisabled = true
	if err := st.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Resolve(ctx, tok); p != nil {
		t.Error("a disabled user's token still works")
	}
	u.IsDisabled = false
	if err := st.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-DeviceIdleExpiry - time.Hour).UTC().Format(store.TimeFormat)
	if _, err := st.DB().ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, stale, d.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Resolve(ctx, tok); p != nil {
		t.Error("a token unused past DeviceIdleExpiry still works")
	}
	if dev, _ := st.GetDeviceByTokenHash(ctx, HashToken(tok)); dev != nil {
		t.Error("the idle device was not removed")
	}
	// A new sign-in, then signed out.
	d2, tok2, err := s.IssueToken(ctx, u, ClientInfo{Client: "test", DeviceID: "d2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDevice(ctx, d2.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Resolve(ctx, tok2); p != nil {
		t.Error("a signed-out device's token still works")
	}
}
