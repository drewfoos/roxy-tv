package auth

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Password rules: long enough to resist guessing, short enough for bcrypt (72 bytes).
const (
	MinPasswordLen   = 8
	maxPasswordBytes = 72
)

var (
	ErrWrongPassword = errors.New("the current password is wrong")
	ErrWeakPassword  = errors.New("the new password must be at least 8 characters")
	ErrLongPassword  = errors.New("the new password is too long (72 bytes at most)")
	ErrSamePassword  = errors.New("the new password is the same as the current one")
)

// ValidateNewPassword checks a password someone is about to set.
func ValidateNewPassword(pw string) error {
	if utf8.RuneCountInString(strings.TrimSpace(pw)) < MinPasswordLen {
		return ErrWeakPassword
	}
	if len(pw) > maxPasswordBytes {
		return ErrLongPassword
	}
	return nil
}

// ChangePassword replaces a user's password after checking the current one. Wrong
// current passwords count toward the same sign-in throttle, so this is no back door for
// guessing. Every other signed-in device is signed out; keepDevice (the caller) stays.
func (s *Service) ChangePassword(ctx context.Context, userID, keepDevice, address, current, next string) error {
	u, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrInvalidCredentials
	}
	ak := addrKey(address)
	uk := userKey(u.Name) + "|" + ak // per address, as at sign-in
	if wait := s.throttle.wait(uk, ak); wait > 0 {
		return &RetryError{After: wait}
	}
	if !CheckPassword(u.PasswordHash, current) {
		s.throttle.fail(uk, ak)
		return ErrWrongPassword
	}
	if err := ValidateNewPassword(next); err != nil {
		return err
	}
	if next == current {
		return ErrSamePassword
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHash(ctx, u.ID, h); err != nil {
		return err
	}
	s.throttle.succeed(uk)
	return s.store.DeleteDevicesExcept(ctx, u.ID, keepDevice)
}
