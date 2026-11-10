// Package auth handles passwords, opaque device tokens, API keys, and request
// authentication for both the native and Jellyfin-compatible APIs.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type Service struct {
	store    *store.Store
	throttle *loginThrottle
	mk       mediaKey
}

func New(st *store.Store) *Service { return &Service{store: st, throttle: newLoginThrottle()} }

// PasswordCost is bcrypt's work factor (2^12 rounds, ~250 ms on a desktop CPU). bcrypt
// salts every hash itself; hashes made with a lower cost are upgraded at the next sign-in.
const PasswordCost = 12

// HashPassword hashes a password with bcrypt (salted, slow on purpose). bcrypt reads at
// most 72 bytes, so longer passwords are refused rather than silently cut.
func HashPassword(pw string) (string, error) {
	if pw == "" {
		return "", errors.New("a password is required")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), PasswordCost)
	return string(b), err
}

// CheckPassword reports whether pw matches hash. An account with no hash never matches:
// a blank password is not a password.
func CheckPassword(hash, pw string) bool {
	if hash == "" || pw == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash is compared against when the account does not exist, so a wrong username
// takes as long as a wrong password and response times don't reveal which names exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("roxy-timing-equaliser"), PasswordCost)

// NewToken returns a random 32-byte token as 64 hex chars (what clients store) .
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Login verifies credentials and issues a device token.
// Wrong passwords are throttled per account and per address (ErrTooManyAttempts).
func (s *Service) Login(ctx context.Context, username, password string, client ClientInfo) (*model.User, *model.Device, string, error) {
	// The account's count is per address: wrong guesses from elsewhere must not lock the
	// owner out (5 misses from anywhere used to block even the right password, for up to
	// 15 minutes at a time). The address's own count still stops one place guessing.
	ak := addrKey(client.Address)
	uk := userKey(username) + "|" + ak
	gk := globalKey(username)
	if wait := s.throttle.wait(uk, ak); wait > 0 {
		return nil, nil, "", &RetryError{After: wait}
	}
	// Many addresses guessing one account: each try waits a little longer (no lockout).
	if d := s.throttle.accountDelay(gk); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, nil, "", ctx.Err()
		}
	}
	u, err := s.store.GetUserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		return nil, nil, "", err
	}
	if u == nil || u.IsDisabled || u.PasswordHash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password)) // same time as a real check
		s.throttle.fail(uk, ak, gk)
		return nil, nil, "", ErrInvalidCredentials
	}
	if !CheckPassword(u.PasswordHash, password) {
		s.throttle.fail(uk, ak, gk)
		return nil, nil, "", ErrInvalidCredentials
	}
	s.throttle.succeed(uk)
	s.throttle.succeed(gk)
	// Upgrade an older, cheaper hash now that we know the password.
	if cost, err := bcrypt.Cost([]byte(u.PasswordHash)); err == nil && cost < PasswordCost {
		if h, err := HashPassword(password); err == nil {
			if err := s.store.SetPasswordHash(ctx, u.ID, h); err == nil {
				u.PasswordHash = h
			}
		}
	}
	d, token, err := s.IssueToken(ctx, u, client)
	if err != nil {
		return nil, nil, "", err
	}
	return u, d, token, nil
}

// IssueToken creates a device row and its token for a user already proven to be
// present: after a password check, or when a signed-in phone approves a TV's link code.
func (s *Service) IssueToken(ctx context.Context, u *model.User, client ClientInfo) (*model.Device, string, error) {
	token := NewToken()
	d := &model.Device{
		ID:            newID(),
		UserID:        u.ID,
		Name:          client.Device,
		Client:        client.Client,
		ClientVersion: client.Version,
		DeviceID:      client.DeviceID,
		LastIP:        client.Address,
		TokenHash:     HashToken(token),
	}
	if err := s.store.CreateDevice(ctx, d); err != nil {
		return nil, "", err
	}
	return d, token, nil
}

// CreateAPIKey mints an integration key (the Sonarr and Radarr webhooks).
func (s *Service) CreateAPIKey(ctx context.Context, name string) (*model.APIKey, error) {
	token := NewToken()
	k := &model.APIKey{ID: newID(), Name: name, TokenHash: HashToken(token), Token: token}
	if err := s.store.CreateAPIKey(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

// Resolve turns a bearer token into a Principal, or nil if unknown.
func (s *Service) Resolve(ctx context.Context, token string) (*model.Principal, error) {
	return s.ResolveFrom(ctx, token, "")
}

// ResolveFrom is Resolve for a request from ip, which a device token records as the
// device's last address (Device Management).
func (s *Service) ResolveFrom(ctx context.Context, token, ip string) (*model.Principal, error) {
	if token == "" {
		return nil, nil
	}
	hash := HashToken(token)
	if d, err := s.store.GetDeviceByTokenHash(ctx, hash); err != nil {
		return nil, err
	} else if d != nil {
		u, err := s.store.GetUser(ctx, d.UserID)
		if err != nil || u == nil || u.IsDisabled {
			return nil, err
		}
		// A sign-in nobody has used for half a year is a lost or forgotten device: it ends.
		if !d.LastSeen.IsZero() && time.Since(d.LastSeen) > DeviceIdleExpiry {
			_ = s.store.DeleteDevice(ctx, d.ID)
			return nil, nil
		}
		s.store.TouchDevice(ctx, d.ID, ip)
		return &model.Principal{UserID: u.ID, User: u, DeviceID: d.ID, IsAdmin: u.IsAdmin, Token: token}, nil
	}
	if k, err := s.store.GetAPIKeyByHash(ctx, hash); err != nil {
		return nil, err
	} else if k != nil {
		return &model.Principal{IsAPIKey: true, Token: token}, nil
	}
	return nil, nil
}

// ClientInfo is what the MediaBrowser Authorization header carries about the caller.
type ClientInfo struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
	Address  string // the caller's IP, for sign-in throttling
}

// RetryError is ErrTooManyAttempts with how long to wait.
type RetryError struct{ After time.Duration }

func (e *RetryError) Error() string { return ErrTooManyAttempts.Error() }
func (e *RetryError) Unwrap() error { return ErrTooManyAttempts }

// RemoteIP is the request's client address without the port.
func RemoteIP(r *http.Request) string {
	a := r.RemoteAddr
	if i := strings.LastIndex(a, ":"); i > 0 && !strings.HasSuffix(a, "]") {
		a = a[:i]
	}
	return strings.Trim(a, "[]")
}

// TokenFromRequest extracts the credential from any of the places Jellyfin clients put it.
// SessionCookie carries a device token for browser-fetched resources (Roxy Web). Set
// by POST /v1/auth/cookie, cleared by logout, honoured for GET and HEAD only.
const SessionCookie = "roxy_token"

// SetSessionCookie writes the cookie for token: HttpOnly, same-site strict, a year long,
// Secure when the request came over TLS (directly or through a proxy).
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600})
}

// ClearSessionCookie removes it (logout).
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func TokenFromRequest(r *http.Request) (string, ClientInfo) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		auth = r.Header.Get("X-Emby-Authorization")
	}
	fields := ParseMediaBrowserAuth(auth)
	ci := ClientInfo{Client: fields["Client"], Device: fields["Device"], DeviceID: fields["DeviceId"], Version: fields["Version"]}

	if t := fields["Token"]; t != "" {
		return t, ci
	}
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:]), ci
	}
	for _, h := range []string{"X-MediaBrowser-Token", "X-Emby-Token", "X-Api-Key"} {
		if t := r.Header.Get(h); t != "" {
			return t, ci
		}
	}
	// A token in the URL only on the webhook routes, where Sonarr / Radarr may be set up
	// with ?api_key= and only the webhook key works anyway. Anywhere else a URL token is
	// a full account token waiting in a proxy log or browser history; no Roxy client
	// sends one (the web app has its cookie, players have media tokens).
	if APIKeyPath(r.URL.Path) {
		for _, q := range []string{"api_key", "ApiKey", "apiKey"} {
			if t := r.URL.Query().Get(q); t != "" {
				return t, ci
			}
		}
	}
	// Roxy Web: the browser loads posters and video on its own and cannot add a header,
	// so a same-site cookie carries the device token. Reads only: a change still needs
	// the Bearer header, which keeps a cross-site page from acting through the cookie.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			return c.Value, ci
		}
	}
	return "", ci
}

// ParseMediaBrowserAuth parses `MediaBrowser Client="x", Device="y", DeviceId="z", Version="1", Token="t"`.
func ParseMediaBrowserAuth(header string) map[string]string {
	out := map[string]string{}
	header = strings.TrimSpace(header)
	lower := strings.ToLower(header)
	if !strings.HasPrefix(lower, "mediabrowser") && !strings.HasPrefix(lower, "emby") {
		return out
	}
	sp := strings.IndexAny(header, " \t")
	if sp < 0 {
		return out
	}
	for _, kv := range strings.Split(header[sp+1:], ",") {
		kv = strings.TrimSpace(kv)
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			continue
		}
		out[strings.TrimSpace(kv[:eq])] = strings.Trim(strings.TrimSpace(kv[eq+1:]), `"`)
	}
	return out
}

// ---- request context ----

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *model.Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// ErrProfileGone: the request names a profile that does not exist or is another login's.
var ErrProfileGone = errors.New("profile not found")

// ProfileGoneHeader marks the 409 that answers such a request, so a client can tell it
// from the other conflicts and go back to "Who's watching?".
const ProfileGoneHeader = "X-Roxy-Profile-Gone"

// resolveScope picks the profile that keys this request's watch state: the one named by
// the X-Roxy-Profile header, or the user's default when the request names none. A named
// profile that is not the user's (deleted on another device, or another login's) is
// ErrProfileGone: it used to mean the default profile too, so a device still holding a
// deleted profile went on showing its name while reading and writing the default
// profile's watch state. Only where anyProfile is set does it still mean the default.
func (s *Service) resolveScope(ctx context.Context, userID, requested string, anyProfile bool) (string, error) {
	if requested != "" {
		p, err := s.store.GetProfile(ctx, requested)
		if err != nil {
			return "", err
		}
		if p != nil && p.UserID == userID {
			return p.ID, nil
		}
		if !anyProfile {
			return "", ErrProfileGone
		}
	}
	return s.store.DefaultProfileID(ctx, userID)
}

// ProfileFreePath: the routes that answer whatever profile the request names, because a
// client needs them to get out of that state (list the profiles and pick one, sign out,
// update itself) and none of them reads or writes a profile's watch state.
func ProfileFreePath(path string) bool {
	if path == "/v1/me/profiles" || path == "/v1/me/password" {
		return true
	}
	for _, prefix := range []string{"/v1/me/profiles/", "/v1/auth/", "/v1/avatars/", "/v1/apps/", "/v1/legal/", "/v1/link/", "/v1/diagnostics/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func PrincipalFrom(ctx context.Context) *model.Principal {
	p, _ := ctx.Value(ctxKey{}).(*model.Principal)
	return p
}

// Middleware resolves the caller. If required is true, unauthenticated requests get 401.
func (s *Service) Middleware(required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, _ := TokenFromRequest(r)
			p, err := s.ResolveFrom(r.Context(), token, RemoteIP(r))
			if err != nil {
				http.Error(w, `{"Message":"auth lookup failed"}`, http.StatusInternalServerError)
				return
			}
			// API keys exist for the Sonarr / Radarr webhooks and do nothing else: a key read
			// out of an arr app's settings used to open the whole library and its streams.
			if p != nil && p.IsAPIKey && !APIKeyPath(r.URL.Path) {
				p = nil
			}
			// A media token opens one source's stream and what plays with it, nothing else. It
			// comes as ?mt= (the browser's <video> cannot send headers) or as the
			// X-Roxy-Media-Token header (the TV player: it keeps the token out of URLs, which
			// libmpv writes to the device log).
			if p == nil {
				mt := r.Header.Get(MediaTokenHeader)
				if mt == "" {
					mt = r.URL.Query().Get("mt")
				}
				if mt != "" {
					if src, uid, err := s.verifyMediaToken(r.Context(), mt); err == nil && mediaPathFor(r.URL.Path, src) {
						if u, err := s.store.GetUser(r.Context(), uid); err == nil && u != nil && !u.IsDisabled {
							p = &model.Principal{UserID: u.ID, User: u, MediaSource: src}
						}
					}
				}
			}
			if p == nil && required {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"Message":"Unauthorized"}`))
				return
			}
			if p != nil {
				if p.User != nil && p.MediaSource == "" {
					scope, err := s.resolveScope(r.Context(), p.UserID, r.Header.Get("X-Roxy-Profile"), ProfileFreePath(r.URL.Path))
					if errors.Is(err, ErrProfileGone) {
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set(ProfileGoneHeader, "1")
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"error":"That profile no longer exists. Choose a profile again.","code":"profile_gone"}`))
						return
					}
					if err != nil {
						http.Error(w, `{"Message":"profile lookup failed"}`, http.StatusInternalServerError)
						return
					}
					p.ScopeID = scope
				}
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MediaTokenHeader carries a media token in place of the ?mt= query parameter.
const MediaTokenHeader = "X-Roxy-Media-Token"

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// APIKeyPath: the routes an integration API key may call (the arr webhooks).
func APIKeyPath(path string) bool {
	return strings.HasPrefix(path, "/v1/webhooks/") && path != "/v1/webhooks/events" || strings.HasPrefix(path, "/v1/ingest/")
}

// DeviceIdleExpiry: a device token unused this long stops working.
const DeviceIdleExpiry = 180 * 24 * time.Hour
