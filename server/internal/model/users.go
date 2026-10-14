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

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID   string
	User     *User
	DeviceID string // Roxy device row id (empty for API keys)
	IsAPIKey bool
	IsAdmin  bool
	Token    string
}
