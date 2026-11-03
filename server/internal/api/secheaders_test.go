package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSOnlyOnPublicProbe(t *testing.T) {
	h := cors(securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })))
	for path, want := range map[string]string{
		"/v1/system/info": "*",
		"/healthz":        "*",
		"/v1/auth/me":     "",
		"/v1/items":       "",
		"/":               "",
	} {
		for _, m := range []string{http.MethodGet, http.MethodOptions} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(m, path, nil)
			req.Header.Set("Origin", "https://evil.example")
			h.ServeHTTP(rec, req)
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
				t.Errorf("%s %s: Allow-Origin %q, want %q", m, path, got, want)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" && m == http.MethodGet {
				t.Errorf("%s %s: no nosniff", m, path)
			}
		}
	}
}
