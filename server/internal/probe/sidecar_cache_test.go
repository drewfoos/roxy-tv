package probe

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A sidecar replaced within the same second (another millisecond or another size) gets a
// new cache name; the name used whole seconds, so the old file's conversion was served.
func TestSidecarCachePathChangesWithTheFile(t *testing.T) {
	at := time.Date(2026, 11, 7, 9, 0, 0, 100*int(time.Millisecond), time.UTC)
	st := model.MediaStream{Index: 7, IsExternal: true, Codec: "sami", ExternalModTime: at, ExternalSize: 1000}
	first := SidecarCachePath("/c", "src", st)
	st.ExternalModTime = at.Add(400 * time.Millisecond) // same second
	if again := SidecarCachePath("/c", "src", st); again == first {
		t.Fatalf("same name after a change within the second: %s", again)
	}
	st.ExternalModTime = at
	st.ExternalSize = 1200 // same time, other content
	if again := SidecarCachePath("/c", "src", st); again == first {
		t.Fatalf("same name for a file of another size: %s", again)
	}
	st.ExternalSize = 1000
	if again := SidecarCachePath("/c", "src", st); again != first {
		t.Fatalf("an unchanged file must keep its name: %s vs %s", again, first)
	}
	if p := SidecarCachePath("/c", "src", model.MediaStream{IsExternal: true, Codec: "ass"}); p != "" {
		t.Fatalf("a plain .ass sidecar is served as it is, not cached: %q", p)
	}
}
