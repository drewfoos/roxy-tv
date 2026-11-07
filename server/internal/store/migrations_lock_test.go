package store

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Every migration that has shipped is pinned by its checksum in migrations.lock: databases
// out there already ran its text, so editing it would leave them (and any historical
// fixture built from these files) different from what the code now expects. A new
// migration must be added to the lock; an edited old one fails here.
func TestMigrationsUnchanged(t *testing.T) {
	b, err := os.ReadFile("migrations.lock")
	if err != nil {
		t.Fatal(err)
	}
	locked := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(line, "#") {
			locked[f[1]] = f[0]
		}
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		seen[name] = true
		body, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		want, ok := locked[name]
		switch {
		case !ok:
			t.Errorf("%s is not in migrations.lock; once it is final, add:\n%s  %s", name, got, name)
		case want != got:
			t.Errorf("%s has changed since it shipped (lock %s, now %s): write a new migration instead", name, want[:12], got[:12])
		}
	}
	for name := range locked {
		if !seen[name] {
			t.Errorf("%s is in migrations.lock but gone from migrations/: a shipped migration cannot be removed", name)
		}
	}
}
