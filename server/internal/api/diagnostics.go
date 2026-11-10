package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/roxy-media/roxy/server/internal/auth"
)

// maxCrashBody is the most of a crash report's request that is read. The app sends up to
// 60 000 characters of report, which JSON escaping (every line break and tab of a stack
// trace is two bytes) can double; the 16 KB this used to be cut most real reports short,
// they were refused as invalid, and the app (which keeps a report until it is accepted)
// sent the same one at every start.
const maxCrashBody = 256 << 10

// POST /v1/diagnostics/crash {report}: an app's crash from its previous run, logged so
// it can be read from the server's log (docker logs roxy | grep "client crash").
func (s *Server) clientCrash(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Report string `json:"report"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxCrashBody)).Decode(&body); err != nil || body.Report == "" {
		writeError(w, 400, "invalid request body")
		return
	}
	user := ""
	if p := auth.PrincipalFrom(r.Context()); p != nil && p.User != nil {
		user = p.User.Name
	}
	slog.Error("client crash", "user", user, "from", auth.RemoteIP(r), "report", body.Report)
	w.WriteHeader(204)
}
