package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Signing a TV in from a phone. The TV asks for a link code and shows it as a QR code
// (a link into the web app) and as text; whoever is signed in on the phone opens the
// link, sees which device is asking and approves it; the TV, long-polling with the
// code's secret, receives a device token of its own and is signed in. Codes live ten
// minutes, in memory: a restart just makes the TV ask for a new one.
//
//	POST /v1/link/start {client, device, deviceId, version}   (public)
//	  -> {code, secret, url, expiresIn}
//	GET  /v1/link/{code}/qr.png?secret=                       (public) the QR, 320 px
//	GET  /v1/link/{code}/wait?secret=                         (public) long-poll, up to 25 s
//	  -> {status: "pending"} | {status: "done", token, user, deviceId}
//	GET  /v1/link/{code}                                      (signed in) which device asks
//	POST /v1/link/{code}/approve                              (signed in) sign that device in

const (
	linkTTL      = 10 * time.Minute
	linkWaitMax  = 25 * time.Second
	linkCodeLen  = 6
	linkAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
)

type linkEntry struct {
	code      string
	secret    string
	client    auth.ClientInfo
	createdAt time.Time
	// Set by approve: what the TV receives once.
	token    string
	user     *model.User
	deviceID string
	done     chan struct{}
	// claimed: an approval is under way (or done). Checked and set under the store's lock,
	// so two approvals racing can't both issue a token and both close done (which panicked
	// while holding the lock and left every later /link request hanging).
	claimed bool
}

type linkStore struct {
	mu      sync.Mutex
	entries map[string]*linkEntry
}

func newLinkStore() *linkStore {
	ls := &linkStore{entries: map[string]*linkEntry{}}
	go func() {
		for range time.Tick(time.Minute) {
			ls.mu.Lock()
			for k, e := range ls.entries {
				if time.Since(e.createdAt) > linkTTL {
					delete(ls.entries, k)
				}
			}
			ls.mu.Unlock()
		}
	}()
	return ls
}

func (ls *linkStore) get(code string) *linkEntry {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	e := ls.entries[strings.ToUpper(strings.ReplaceAll(code, "-", ""))]
	if e == nil || time.Since(e.createdAt) > linkTTL {
		return nil
	}
	return e
}

func randomString(alphabet string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// linkURL is where the phone goes: the advertised address when one is set (Docker,
// proxies), else the host this request came in on.
func (s *Server) linkURL(r *http.Request, code string) string {
	base := s.cfg.AdvertiseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return strings.TrimRight(base, "/") + "/link/" + code
}

func (s *Server) linkStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Client   string `json:"client"`
		Device   string `json:"device"`
		DeviceID string `json:"deviceId"`
		Version  string `json:"version"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
	if len(body.Device) > 128 || len(body.Client) > 64 || len(body.DeviceID) > 128 || len(body.Version) > 64 {
		writeError(w, 400, "invalid request body")
		return
	}
	if body.Client == "" {
		body.Client = "Roxy TV"
	}
	if body.Device == "" {
		body.Device = "TV"
	}
	e := &linkEntry{
		secret:    randomString("abcdefghijklmnopqrstuvwxyz0123456789", 32),
		client:    auth.ClientInfo{Client: body.Client, Device: body.Device, DeviceID: body.DeviceID, Version: body.Version, Address: auth.RemoteIP(r)},
		createdAt: time.Now(),
		done:      make(chan struct{}),
	}
	s.links.mu.Lock()
	for {
		e.code = randomString(linkAlphabet, linkCodeLen)
		if _, taken := s.links.entries[e.code]; !taken {
			break
		}
	}
	s.links.entries[e.code] = e
	s.links.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"code":      e.code,
		"secret":    e.secret,
		"url":       s.linkURL(r, e.code),
		"expiresIn": int(linkTTL.Seconds()),
	})
}

// linkEntryFor finds the code and checks its secret; nil (and a 404) otherwise.
func (s *Server) linkEntryFor(w http.ResponseWriter, r *http.Request) *linkEntry {
	e := s.links.get(chi.URLParam(r, "code"))
	if e == nil || subtle.ConstantTimeCompare([]byte(e.secret), []byte(r.URL.Query().Get("secret"))) != 1 {
		writeError(w, 404, "code expired or unknown")
		return nil
	}
	return e
}

func (s *Server) linkQR(w http.ResponseWriter, r *http.Request) {
	e := s.linkEntryFor(w, r)
	if e == nil {
		return
	}
	png, err := qrcode.Encode(s.linkURL(r, e.code), qrcode.Medium, 320)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) linkWait(w http.ResponseWriter, r *http.Request) {
	e := s.linkEntryFor(w, r)
	if e == nil {
		return
	}
	select {
	case <-e.done:
	case <-time.After(linkWaitMax):
		writeJSON(w, 200, map[string]any{"status": "pending"})
		return
	case <-r.Context().Done():
		return
	}
	// Handed over once: the code is gone after this.
	s.links.mu.Lock()
	delete(s.links.entries, e.code)
	s.links.mu.Unlock()
	writeJSON(w, 200, map[string]any{"status": "done", "token": e.token, "user": userJSON(e.user), "deviceId": e.deviceID})
}

// GET /v1/link/{code}: the phone side asks which device wants in before approving.
func (s *Server) linkInfo(w http.ResponseWriter, r *http.Request) {
	e := s.links.get(chi.URLParam(r, "code"))
	if e == nil {
		writeError(w, 404, "This code has expired. Ask the TV for a new one.")
		return
	}
	s.links.mu.Lock()
	approved := e.token != ""
	s.links.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"code":      e.code,
		"client":    e.client.Client,
		"device":    e.client.Device,
		"expiresIn": int((linkTTL - time.Since(e.createdAt)).Seconds()),
		"approved":  approved,
		// Where the device asking is (home / tailscale / internet): a code started from the
		// internet and sent to someone as a link is the phishing case.
		"network": networkOf(e.client.Address),
	})
}

// POST /v1/link/{code}/approve: the signed-in user lends the device a token of its own.
func (s *Server) linkApprove(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || p.IsAPIKey || p.User == nil {
		writeError(w, 403, "sign in as a user to approve a device")
		return
	}
	e := s.links.get(chi.URLParam(r, "code"))
	if e == nil {
		writeError(w, 404, "This code has expired. Ask the TV for a new one.")
		return
	}
	s.links.mu.Lock()
	if e.claimed {
		s.links.mu.Unlock()
		writeError(w, 409, "already approved")
		return
	}
	e.claimed = true
	s.links.mu.Unlock()
	d, token, err := s.authSvc.IssueToken(r.Context(), p.User, e.client)
	if err != nil {
		s.links.mu.Lock()
		e.claimed = false
		s.links.mu.Unlock()
		writeError(w, 500, err.Error())
		return
	}
	s.links.mu.Lock()
	e.token, e.user, e.deviceID = token, p.User, d.ID
	s.links.mu.Unlock()
	close(e.done) // once: only the one claim gets here
	slog.Info("device linked", "user", p.User.Name, "device", e.client.Device, "client", e.client.Client, "by", auth.RemoteIP(r))
	w.WriteHeader(204)
}
