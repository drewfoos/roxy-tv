package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Media tokens: what goes in a video URL instead of the account's sign-in token. A
// player (libmpv, an external app on the TV) can't send headers, so the credential has to
// ride in the URL, where it can end up in another app or a log. A media token unlocks one
// source's stream, transcode, subtitles, fonts and previews for a day, and nothing else:
// no account API, no admin actions, no other files.
//
// Format: base64url(sourceID "." userID "." expiryUnix) "." hex(HMAC-SHA256). The key is
// random per server, kept in the settings table; replacing it voids every media token.

const MediaTokenTTL = 24 * time.Hour

var errBadMediaToken = errors.New("invalid media token")

type mediaKey struct {
	once sync.Once
	key  []byte
	err  error
}

func (s *Service) mediaSecret(ctx context.Context) ([]byte, error) {
	s.mk.once.Do(func() {
		var hexKey string
		ok, err := s.store.GetSetting(ctx, "media_token_key", &hexKey)
		if err == nil && ok && len(hexKey) == 64 {
			s.mk.key, s.mk.err = hex.DecodeString(hexKey)
			return
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			s.mk.err = err
			return
		}
		s.mk.key = b
		s.mk.err = s.store.SetSetting(ctx, "media_token_key", hex.EncodeToString(b))
	})
	return s.mk.key, s.mk.err
}

// NewMediaToken signs a token for userID to play sourceID until now + MediaTokenTTL. It
// names the device that asked (deviceID), so signing that device out voids it at once.
func (s *Service) NewMediaToken(ctx context.Context, sourceID, userID, deviceID string) (string, time.Time, error) {
	key, err := s.mediaSecret(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(MediaTokenTTL)
	payload := sourceID + "." + userID + "." + deviceID + "." + strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(mac.Sum(nil)), exp, nil
}

// verifyMediaToken returns the source and user a token was issued for.
func (s *Service) verifyMediaToken(ctx context.Context, tok string) (sourceID, userID string, err error) {
	key, err := s.mediaSecret(ctx)
	if err != nil {
		return "", "", err
	}
	dot := strings.LastIndex(tok, ".")
	if dot <= 0 {
		return "", "", errBadMediaToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok[:dot])
	if err != nil {
		return "", "", errBadMediaToken
	}
	sig, err := hex.DecodeString(tok[dot+1:])
	if err != nil {
		return "", "", errBadMediaToken
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", "", errBadMediaToken
	}
	parts := strings.Split(string(raw), ".")
	device := ""
	switch len(parts) {
	case 3: // issued before tokens named their device; they expire within a day
	case 4:
		device = parts[2]
		parts = []string{parts[0], parts[1], parts[3]}
	default:
		return "", "", errBadMediaToken
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", "", errBadMediaToken
	}
	// The device that asked must still be signed in (logout, Device Management's sign
	// out and a password change all end its streams too).
	if device != "" {
		if ok, err := s.store.DeviceExists(ctx, device); err != nil || !ok {
			return "", "", errBadMediaToken
		}
	}
	return parts[0], parts[1], nil
}

// mediaPathFor reports whether path is one of the routes a media token may open for
// sourceID: the stream itself and what a player loads alongside it.
func mediaPathFor(path, sourceID string) bool {
	if sourceID == "" || strings.ContainsAny(sourceID, "/.") {
		return false
	}
	if path == "/v1/stream/"+sourceID {
		return true
	}
	rest, ok := strings.CutPrefix(path, "/v1/sources/"+sourceID+"/")
	if !ok {
		return false
	}
	for _, p := range []string{"transcode", "remux", "subtitles/", "attachments/", "trickplay"} {
		if rest == p || strings.HasPrefix(rest, p) {
			return true
		}
	}
	return false
}
