package library

import (
	"os"
	"testing"
)

// skipUnlessRequired skips a test whose requirements are missing (ffmpeg, a non-root
// user), except where ROXY_REQUIRE_INTEGRATION is set (the CI integration job): there a
// test that cannot run fails, so a green run means it ran.
func skipUnlessRequired(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("ROXY_REQUIRE_INTEGRATION") != "" {
		t.Fatalf("ROXY_REQUIRE_INTEGRATION is set but this test cannot run: %s", reason)
	}
	t.Skip(reason)
}
