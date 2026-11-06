package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

func profileRouter(s *Server, userID string) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			p := &model.Principal{UserID: userID, User: &model.User{ID: userID}}
			next.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), p)))
		})
	})
	r.Get("/v1/me/profiles/{id}/avatar", s.profileAvatar)
	r.Patch("/v1/me/profiles/{id}", s.updateProfile)
	return r
}

// ?w= on an avatar snaps to the fixed image widths, so arbitrary widths cannot each
// leave a cached file behind.
func TestAvatarWidthSnapsToFixedSizes(t *testing.T) {
	s, st, prof, red, _, dir := avatarFixture(t)
	ctx := context.Background()
	if err := st.UpdateProfile(ctx, prof.ID, "", red, "Red"); err != nil {
		t.Fatal(err)
	}
	p, _ := st.GetProfile(ctx, prof.ID)
	s.fetchAvatar(ctx, p)
	h := profileRouter(s, prof.UserID)
	for _, w := range []int{16, 17, 99, 100, 150, 159, 161, 200, 300, 319} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/me/profiles/%s/avatar?w=%d", prof.ID, w), nil))
		if rec.Code != 200 {
			t.Fatalf("w=%d: status %d", w, rec.Code)
		}
	}
	variants, _ := filepath.Glob(filepath.Join(dir, "*.w*.jpg"))
	if len(variants) != 2 {
		t.Fatalf("variants cached: %v, want the 160 and 320 sizes only", variants)
	}
	for _, v := range variants {
		if !strings.HasSuffix(v, ".w160.jpg") && !strings.HasSuffix(v, ".w320.jpg") {
			t.Errorf("unexpected variant %s", v)
		}
	}
}

// A profile deleted between the update and the re-read answers 404; it used to crash
// the handler on a nil profile.
func TestUpdateProfileDeletedMeanwhile(t *testing.T) {
	s, st, prof, _, _, _ := avatarFixture(t)
	ctx := context.Background()
	if _, err := st.CreateProfile(ctx, prof.UserID, "Coffee", "", ""); err != nil {
		t.Fatal(err)
	}
	profileBeforeReload = func() {
		if err := st.DeleteProfile(ctx, prof.ID); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { profileBeforeReload = func() {} })
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("handler panicked: %v", r)
			}
		}()
		profileRouter(s, prof.UserID).ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/me/profiles/"+prof.ID, strings.NewReader(`{"name":"Renamed"}`)))
	}()
	if rec.Code != 404 {
		t.Fatalf("status %d (%s), want 404", rec.Code, rec.Body.String())
	}
}
