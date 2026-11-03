package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckPasswordRefusesBlank(t *testing.T) {
	if CheckPassword("", "") || CheckPassword("", "x") {
		t.Fatal("an account without a hash must never match")
	}
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") || CheckPassword(h, "") || CheckPassword(h, "wrong") {
		t.Fatal("bcrypt check wrong")
	}
	if _, err := HashPassword(""); err == nil {
		t.Fatal("a blank password must be refused")
	}
}

func TestThrottleLocksAfterFreeAttempts(t *testing.T) {
	th := &loginThrottle{keys: map[string]*attempts{}}
	k := userKey("Roxy")
	for i := 0; i < freeAttempts-1; i++ {
		th.fail(k)
		if th.wait(k) > 0 {
			t.Fatalf("locked after %d misses", i+1)
		}
	}
	th.fail(k)
	if w := th.wait(k); w < 25*time.Second || w > firstLockout {
		t.Fatalf("after %d misses wait = %v, want ~%v", freeAttempts, w, firstLockout)
	}
	th.fail(k)
	if w := th.wait(k); w < 55*time.Second {
		t.Fatalf("lockout should double, got %v", w)
	}
	th.succeed(k)
	if th.wait(k) > 0 {
		t.Fatal("a correct password clears the account")
	}
}

func TestMediaPathFor(t *testing.T) {
	ok := []string{"/v1/stream/abc", "/v1/sources/abc/transcode", "/v1/sources/abc/subtitles/2", "/v1/sources/abc/attachments/1", "/v1/sources/abc/trickplay", "/v1/sources/abc/trickplay/0"}
	for _, p := range ok {
		if !mediaPathFor(p, "abc") {
			t.Errorf("%s should be allowed", p)
		}
	}
	bad := []string{"/v1/stream/abd", "/v1/sources/abc", "/v1/sources/abc/token", "/v1/home", "/v1/sources/abcd/transcode", "/v1/settings/arr"}
	for _, p := range bad {
		if mediaPathFor(p, "abc") {
			t.Errorf("%s must not be allowed", p)
		}
	}
	if mediaPathFor("/v1/stream/..", "..") {
		t.Error("dots must never pass")
	}
}

// Wrong guesses from another address must not lock the owner out of their own account.
func TestLockoutIsPerAddress(t *testing.T) {
	th := &loginThrottle{keys: map[string]*attempts{}}
	attacker := userKey("carol") + "|" + addrKey("10.0.0.66")
	owner := userKey("carol") + "|" + addrKey("192.168.1.20")
	for i := 0; i < freeAttempts+2; i++ {
		th.fail(attacker, addrKey("10.0.0.66"))
	}
	if th.wait(attacker, addrKey("10.0.0.66")) == 0 {
		t.Fatal("the guessing address should be locked")
	}
	if w := th.wait(owner, addrKey("192.168.1.20")); w != 0 {
		t.Fatalf("owner locked out for %v by someone else's guesses", w)
	}
}

func TestAPIKeyPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1/webhooks/sonarr": true, "/v1/ingest/radarr": true,
		"/v1/webhooks/events": false, "/v1/items": false, "/v1/stream/x": false, "/v1/auth/me": false,
	} {
		if APIKeyPath(path) != want {
			t.Errorf("%s: want %v", path, want)
		}
	}
}

// Misses from many addresses slow the account down but never refuse it.
func TestAccountDelayNeverLocks(t *testing.T) {
	th := &loginThrottle{keys: map[string]*attempts{}}
	g := globalKey("carol")
	for i := 0; i < globalFree-1; i++ {
		th.fail(g)
	}
	if th.accountDelay(g) != 0 {
		t.Fatal("delayed before globalFree misses")
	}
	for i := 0; i < 50; i++ {
		th.fail(g)
	}
	if d := th.accountDelay(g); d != maxAccountDelay {
		t.Fatalf("delay %v, want the cap", d)
	}
	if th.wait(userKey("carol")+"|"+addrKey("192.168.1.20"), addrKey("192.168.1.20")) != 0 {
		t.Fatal("the owner's address must not be locked by global misses")
	}
}

// A token in the URL counts only on the webhook routes.
func TestURLTokenOnlyOnWebhooks(t *testing.T) {
	for path, want := range map[string]string{
		"/v1/webhooks/sonarr?api_key=k": "k",
		"/v1/items?api_key=k":           "",
		"/v1/auth/keys?api_key=k":       "",
	} {
		r := httptest.NewRequest("GET", path, nil)
		if got, _ := TokenFromRequest(r); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
	r := httptest.NewRequest("POST", "/v1/webhooks/radarr?api_key=k", nil)
	if got, _ := TokenFromRequest(r); got != "k" {
		t.Errorf("webhook POST with ?api_key=: got %q", got)
	}
}
