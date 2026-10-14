package auth

import "sync"

// Plain-text API keys are kept only in memory for the lifetime of the process, so the
// Jellyfin-compatible /Auth/Keys listing can hand a freshly created key back to request app.
// Keys created in earlier runs are listed without their secret.

var plainKeys sync.Map // token hash -> plain token

func rememberPlainKey(hash, token string) { plainKeys.Store(hash, token) }

func (s *Service) PlainKey(hash string) string {
	if v, ok := plainKeys.Load(hash); ok {
		return v.(string)
	}
	return ""
}
