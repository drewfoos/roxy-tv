package api

import "net/http"

// securityHeaders goes on every response: no MIME sniffing (a JSON or subtitle response
// must never be read as HTML), no Referer leaking Roxy URLs (which can carry ?api_key=)
// to outside image hosts, and no framing (clickjacking the approve-a-TV button). Roxy
// Web's own Content-Security-Policy is set by internal/web; attachments set a stricter one.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// corsPublic lists the only paths another origin may read: another Roxy web app's
// "Change server" probe reads /v1/system/info (and /healthz) to recognise a Roxy server.
// Everything else is same-origin (Roxy Web) or a native app, which needs no CORS.
func corsPublic(path string) bool {
	return path == "/v1/system/info" || path == "/healthz"
}
