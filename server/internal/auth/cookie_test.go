package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The session cookie is a read-only credential: it identifies GET and HEAD requests
// (posters, video, subtitles loaded by the browser itself) and nothing else, so a page
// on another site cannot change anything through it.
func TestSessionCookieReadsOnly(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   string
	}{{http.MethodGet, "tok123"}, {http.MethodHead, "tok123"}, {http.MethodPost, ""}, {http.MethodDelete, ""}} {
		r := httptest.NewRequest(tc.method, "/v1/items/x/images/poster", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "tok123"})
		if got, _ := TokenFromRequest(r); got != tc.want {
			t.Errorf("%s with cookie: token %q, want %q", tc.method, got, tc.want)
		}
	}
	// A header still wins over the cookie.
	r := httptest.NewRequest(http.MethodGet, "/v1/items", nil)
	r.Header.Set("Authorization", "Bearer header-token")
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "cookie-token"})
	if got, _ := TokenFromRequest(r); got != "header-token" {
		t.Errorf("header should win, got %q", got)
	}
}

func TestSetSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/cookie", nil), "abc")
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != SessionCookie || cs[0].Value != "abc" || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || cs[0].Secure {
		t.Fatalf("unexpected cookie %+v", cs)
	}
	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/cookie", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	SetSessionCookie(rec, r, "abc")
	if !rec.Result().Cookies()[0].Secure {
		t.Fatal("behind an HTTPS proxy the cookie must be Secure")
	}
}
