package auth

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrTooManyAttempts: too many wrong passwords for this account or address; RetryAfter says how long.
var ErrTooManyAttempts = errors.New("too many sign-in attempts; try again later")

// loginThrottle slows password guessing. Every wrong password counts against the
// account name and against the caller's address. After freeAttempts misses within the
// window, the next try waits: 30 s, then 1, 2, 4 ... up to 15 minutes. A correct
// password clears the account's count (not the address's, so one known account cannot
// be used to keep guessing others from the same place).
type loginThrottle struct {
	mu   sync.Mutex
	keys map[string]*attempts
}

type attempts struct {
	fails     int
	lastFail  time.Time
	lockedTil time.Time
}

const (
	freeAttempts = 5
	failWindow   = 15 * time.Minute
	firstLockout = 30 * time.Second
	maxLockout   = 15 * time.Minute
)

func newLoginThrottle() *loginThrottle {
	t := &loginThrottle{keys: map[string]*attempts{}}
	go func() {
		for range time.Tick(10 * time.Minute) {
			t.mu.Lock()
			for k, a := range t.keys {
				if time.Since(a.lastFail) > failWindow && time.Now().After(a.lockedTil) {
					delete(t.keys, k)
				}
			}
			t.mu.Unlock()
		}
	}()
	return t
}

func userKey(name string) string { return "u:" + strings.ToLower(strings.TrimSpace(name)) }
func addrKey(ip string) string   { return "a:" + ip }

// wait reports how long the caller must still wait before another try (0 = go ahead).
func (t *loginThrottle) wait(keys ...string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var longest time.Duration
	now := time.Now()
	for _, k := range keys {
		if a := t.keys[k]; a != nil && now.Before(a.lockedTil) {
			if d := a.lockedTil.Sub(now); d > longest {
				longest = d
			}
		}
	}
	return longest
}

func (t *loginThrottle) fail(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		a := t.keys[k]
		if a == nil || now.Sub(a.lastFail) > failWindow {
			a = &attempts{}
			t.keys[k] = a
		}
		a.fails++
		a.lastFail = now
		if over := a.fails - freeAttempts; over >= 0 {
			d := firstLockout << over
			if d > maxLockout || d <= 0 {
				d = maxLockout
			}
			a.lockedTil = now.Add(d)
		}
	}
}

// globalKey: an account's misses from every address together. It never locks (that would
// let anyone lock the owner out); it only slows each attempt down (accountDelay).
func globalKey(name string) string { return "g:" + strings.ToLower(strings.TrimSpace(name)) }

// accountDelay: how long to hold a sign-in for this account before checking it. Free for
// the first globalFree misses in the window, then a second more per miss, up to
// maxAccountDelay: guessing one account from many addresses gets slow, and the owner with
// the right password is only ever delayed, never refused.
func (t *loginThrottle) accountDelay(key string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	a := t.keys[key]
	if a == nil || time.Since(a.lastFail) > failWindow || a.fails < globalFree {
		return 0
	}
	d := time.Duration(a.fails-globalFree+1) * time.Second
	if d > maxAccountDelay {
		d = maxAccountDelay
	}
	return d
}

const (
	globalFree      = 10
	maxAccountDelay = 5 * time.Second
)

func (t *loginThrottle) succeed(userK string) {
	t.mu.Lock()
	delete(t.keys, userK)
	t.mu.Unlock()
}
