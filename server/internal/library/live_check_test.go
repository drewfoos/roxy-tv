package library

import (
	"context"
	"os"
	"testing"

	"github.com/roxy-media/roxy/server/internal/store"
)

// TestLiveIdentityCheck re-resolves every file of a copy of a real database and logs each
// one whose item identity would change: run it before shipping a naming or identity
// change. It writes to the database it is given, so give it a copy:
//
//	ROXY_LIVE_COPY=/tmp/roxy-copy.db go test ./internal/library -run LiveIdentityCheck -v
func TestLiveIdentityCheck(t *testing.T) {
	path := os.Getenv("ROXY_LIVE_COPY")
	if path == "" {
		t.Skip()
	}
	ctx := context.Background()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	libs, _ := st.ListLibraries(ctx)
	l := New(st, nil, nil)
	changed, total := 0, 0
	for _, lib := range libs {
		paths, _ := st.ListPartPaths(ctx, lib.ID)
		for p := range paths {
			old, _ := st.ItemForPath(ctx, p)
			if old == nil {
				continue
			}
			it, err := l.resolveForIngest(ctx, lib, p, nil)
			total++
			if err != nil {
				t.Logf("ERR %s: %v", p, err)
				continue
			}
			if it.GUID != old.GUID {
				changed++
				t.Logf("CHANGED %s\n   %s (S%v E%v A%v) -> %s (S%v E%v A%v)", p, old.GUID, deref(old.SeasonAired), deref(old.IndexAired), deref(old.IndexAbsolute), it.GUID, deref(it.SeasonAired), deref(it.IndexAired), deref(it.IndexAbsolute))
			}
		}
	}
	t.Logf("%d files, %d identities changed", total, changed)
}

func deref(p *int) any {
	if p == nil {
		return "-"
	}
	return *p
}
