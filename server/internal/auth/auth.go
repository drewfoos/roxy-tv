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

	"golang.org/x/crypto/bcrypt"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service { return &Service{store: st} }

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	if hash == "" {
		return pw == ""
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

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
func (s *Service) Login(ctx context.Context, username, password string, client ClientInfo) (*model.User, *model.Device, string, error) {
	u, err := s.store.GetUserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		return nil, nil, "", err
	}
	if u == nil || u.IsDisabled || !CheckPassword(u.PasswordHash, password) {
		return nil, nil, "", ErrInvalidCredentials
	}
	token := NewToken()
	d := &model.Device{
		ID:            newID(),
		UserID:        u.ID,
		Name:          client.Device,
		Client:        client.Client,
		ClientVersion: client.Version,
		DeviceID:      client.DeviceID,
		TokenHash:     HashToken(token),
	}
	if err := s.store.CreateDevice(ctx, d); err != nil {
		return nil, nil, "", err
	}
	return u, d, token, nil
}

// CreateAPIKey mints an integration key (request app, Sonarr, Radarr).
func (s *Service) CreateAPIKey(ctx context.Context, name string) (*model.APIKey, error) {
	token := NewToken()
	k := &model.APIKey{ID: newID(), Name: name, TokenHash: HashToken(token), Token: token}
	if err := s.store.CreateAPIKey(ctx, k); err != nil {
		return nil, err
	}
	rememberPlainKey(k.TokenHash, token)
	return k, nil
}

// Resolve turns a bearer token into a Principal, or nil if unknown.
func (s *Service) Resolve(ctx context.Context, token string) (*model.Principal, error) {
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
		s.store.TouchDevice(ctx, d.ID)
		return &model.Principal{UserID: u.ID, User: u, DeviceID: d.ID, IsAdmin: u.IsAdmin, Token: token}, nil
	}
	if k, err := s.store.GetAPIKeyByHash(ctx, hash); err != nil {
		return nil, err
	} else if k != nil {
		return &model.Principal{IsAPIKey: true, IsAdmin: true, Token: token}, nil
	}
	return nil, nil
}

// ClientInfo is what the MediaBrowser Authorization header carries about the caller.
type ClientInfo struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
}

// TokenFromRequest extracts the credential from any of the places Jellyfin clients put it.
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
	for _, q := range []string{"api_key", "ApiKey", "apiKey"} {
		if t := r.URL.Query().Get(q); t != "" {
			return t, ci
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

func PrincipalFrom(ctx context.Context) *model.Principal {
	p, _ := ctx.Value(ctxKey{}).(*model.Principal)
	return p
}

// Middleware resolves the caller. If required is true, unauthenticated requests get 401.
func (s *Service) Middleware(required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, _ := TokenFromRequest(r)
			p, err := s.Resolve(r.Context(), token)
			if err != nil {
				http.Error(w, `{"Message":"auth lookup failed"}`, http.StatusInternalServerError)
				return
			}
			if p == nil && required {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"Message":"Unauthorized"}`))
				return
			}
			if p != nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
