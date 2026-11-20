package api

import (
	"net/http"
	"strconv"
	"strings"
)

// Which apps and servers can work together (docs/api/native.md "Compatibility").
//
// An app and a server are updated apart: a phone that was off for months meets a server
// updated every week, and a new app can be pointed at a server nobody has updated. Without
// a stated rule the only sign of a mismatch was a page that failed to load.
//
// APILevel counts the server's side: raise it when the API gains something an app may
// come to require. An app holds the lowest level it can work with and says "The server
// needs updating" below it.
//
// MinClientLevel is the oldest app level this server still serves: raise it (to the level
// of the first app that does without the thing) when something older apps rely on is
// removed or changed under them. Apps below it show "Update needed" with the server's own
// update, and are refused with 426 so that nothing half works.
//
// Both go out in /v1/system/info, which an app reads before anything else. That answer,
// the 426 below and the app-update routes are what an old app needs to find its way to a
// new build: they must keep their shape for good.
const (
	APILevel       = 1
	MinClientLevel = 1
)

// ClientLevelHeader is how an app states its own level with every call. A caller that
// sends none (the web app, which is served by this server and so always matches; scripts;
// apps from before the rule) is never refused.
const ClientLevelHeader = "X-Roxy-Api"

// CodeClientTooOld marks the refusal in its JSON body, like the other coded errors.
const CodeClientTooOld = "client_too_old"

func compatInfo() map[string]any {
	return map[string]any{"level": APILevel, "minClient": MinClientLevel}
}

// clientCompat refuses apps older than min, except on the paths such an app needs in
// order to say so and to update itself.
func clientCompat(min int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tooOld(r.Header.Get(ClientLevelHeader), min) && !servedToOldClients(r.URL.Path) {
				w.Header().Set("Upgrade", "roxy-api/"+strconv.Itoa(min))
				writeJSON(w, http.StatusUpgradeRequired, map[string]any{
					"error": "This app is too old for this server. Update the app.",
					"code":  CodeClientTooOld,
					"api":   compatInfo(),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// tooOld: the header names a level below min. No header, or one that is not a number, is
// not a statement about the app and refuses nothing.
func tooOld(header string, min int) bool {
	if header == "" {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(header))
	return err == nil && n < min
}

// servedToOldClients: what an app below the minimum may still ask for. The server's
// identity (where it learns it is too old), the health check, its own update, and signing
// out and in (the update routes need a login).
func servedToOldClients(path string) bool {
	switch path {
	case "/healthz", "/v1/system/info", "/v1/auth/login", "/v1/auth/logout", "/v1/auth/me":
		return true
	}
	return strings.HasPrefix(path, "/v1/apps/")
}
