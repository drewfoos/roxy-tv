package auth

import (
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
