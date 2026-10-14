package model

import "time"

type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"isAdmin"`
	IsDisabled   bool      `json:"isDisabled"`
	CreatedAt    time.Time `json:"createdAt"`
	LastLoginAt  time.Time `json:"lastLoginAt,omitempty"`
}

// Device is a logged-in client; its token is the session credential. Revoke = delete.
type Device struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	Name          string    `json:"name"`
	Client        string    `json:"client"`
	ClientVersion string    `json:"clientVersion"`
	DeviceID      string    `json:"deviceId"` // the client-chosen identifier (Jellyfin DeviceId)
	TokenHash     string    `json:"-"`
	LastSeen      time.Time `json:"lastSeen"`
	CreatedAt     time.Time `json:"createdAt"`
}

type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"-"`
	Token     string    `json:"-"` // only populated at creation / for the compat listing
	CreatedAt time.Time `json:"createdAt"`
}

// MaxProfilesPerUser bounds the picker (Netflix allows five).
const MaxProfilesPerUser = 6

// Profile is one viewer on a login. Watch state, favourites and the home screen are
// per profile; the avatar is an anime character picked from AniList.
type Profile struct {
	ID         string    `json:"id"`
	UserID     string    `json:"-"`
	Name       string    `json:"name"`
	AvatarURL  string    `json:"-"`
	AvatarPath string    `json:"-"`
	AvatarName string    `json:"avatarName,omitempty"`
	IsDefault  bool      `json:"isDefault"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID   string
	User     *User
	DeviceID string // Roxy device row id (empty for API keys)
	IsAPIKey bool
	IsAdmin  bool
	Token    string
	// ScopeID keys watch state: the profile named by X-Roxy-Profile when it belongs to
	// the user, else the user's default profile. Empty for API keys.
	ScopeID string
}
