package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A request naming a profile that is not the caller's (deleted on another device, or
// another login's) is refused with a 409 a client can recognise. It used to be served
// as the caller's default profile, so the device went on under the old name while
// reading and writing another profile's watch state. The routes a client needs to get
// out of that state still answer; a request naming no profile gets the default.
func TestProfileThatIsNotTheCallersIsRefused(t *testing.T) {
	ctx := context.Background()
	s, st, u, _, tok := credFixture(t)
	def, err := st.DefaultProfileID(ctx, u.ID)
	if err != nil || def == "" {
		t.Fatalf("default profile: %q %v", def, err)
	}
	second, err := st.CreateProfile(ctx, u.ID, "Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	other := &model.User{ID: "u2", Name: "bob", Role: model.RoleBasic}
	if err := st.CreateUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	bobs, err := st.DefaultProfileID(ctx, other.ID)
	if err != nil || bobs == "" {
		t.Fatalf("bob's profile: %q %v", bobs, err)
	}
	gone, err := st.CreateProfile(ctx, u.ID, "Gone", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProfile(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	served := ""
	h := s.Middleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = PrincipalFrom(r.Context()).ScopeID
		w.WriteHeader(http.StatusNoContent)
	}))
	do := func(path, profile string) *httptest.ResponseRecorder {
		served = ""
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		if profile != "" {
			r.Header.Set("X-Roxy-Profile", profile)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	for name, profile := range map[string]string{"deleted": gone.ID, "another login's": bobs, "made up": "no-such-profile"} {
		rec := do("/v1/me/progress", profile)
		if rec.Code != http.StatusConflict || rec.Header().Get(ProfileGoneHeader) == "" || !strings.Contains(rec.Body.String(), `"code":"profile_gone"`) {
			t.Errorf("%s profile: %d gone=%q %s, want a marked 409", name, rec.Code, rec.Header().Get(ProfileGoneHeader), rec.Body)
		}
		if served != "" {
			t.Errorf("%s profile: the request was served as profile %q", name, served)
		}
		// Listing the profiles (to pick one again), signing out and updating still work.
		for _, path := range []string{"/v1/me/profiles", "/v1/me/profiles/" + def + "/avatar", "/v1/auth/logout", "/v1/auth/me", "/v1/apps/tv/latest"} {
			if rec := do(path, profile); rec.Code != http.StatusNoContent || served != def {
				t.Errorf("%s profile: %s = %d as %q, want it served as the default profile", name, path, rec.Code, served)
			}
		}
	}
	if rec := do("/v1/me/progress", second.ID); rec.Code != http.StatusNoContent || served != second.ID {
		t.Errorf("own profile: %d as %q, want %q", rec.Code, served, second.ID)
	}
	if rec := do("/v1/me/progress", ""); rec.Code != http.StatusNoContent || served != def {
		t.Errorf("no profile named: %d as %q, want the default %q", rec.Code, served, def)
	}
}

func TestProfileFreePath(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1/me/profiles": true, "/v1/me/profiles/p1": true, "/v1/me/profiles/p1/avatar": true,
		"/v1/auth/me": true, "/v1/auth/logout": true, "/v1/me/password": true, "/v1/apps/tv/latest": true,
		"/v1/avatars/search": true, "/v1/legal/status": true, "/v1/link/ABC123/approve": true,
		"/v1/me/profilesx": false, "/v1/me/progress": false, "/v1/me/played/i1": false, "/v1/me/resume": false,
		"/v1/home": false, "/v1/items": false, "/v1/items/i1": false, "/v1/me/favorite/i1": false, "/v1/me/history": false,
	} {
		if got := ProfileFreePath(path); got != want {
			t.Errorf("ProfileFreePath(%q) = %v, want %v", path, got, want)
		}
	}
}
