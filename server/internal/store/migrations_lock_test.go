package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
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
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		files[e.Name()] = hex.EncodeToString(sum[:])
	}
	for _, p := range checkLock(string(b), files) {
		t.Error(p)
	}
}

var reLockLine = regexp.MustCompile(`^([0-9a-f]{64})  (\S+\.sql)$`)

// checkLock compares the migration files (name -> sha256) with the lock's text and
// describes each problem: a shipped migration changed or removed; a new one not locked,
// or sorting before one already shipped (databases that ran the later one would run it
// after, a new database before: they would differ); a lock line that is malformed, a
// duplicate, or out of order (the lock only grows at the end).
func checkLock(lock string, files map[string]string) []string {
	var problems []string
	locked := map[string]string{}
	last := ""
	for i, line := range strings.Split(lock, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := reLockLine.FindStringSubmatch(line)
		if m == nil {
			problems = append(problems, fmt.Sprintf("migrations.lock line %d is not \"<sha256>  <name>.sql\": %q", i+1, line))
			continue
		}
		name := m[2]
		if _, dup := locked[name]; dup {
			problems = append(problems, fmt.Sprintf("migrations.lock lists %s twice", name))
			continue
		}
		if name <= last {
			problems = append(problems, fmt.Sprintf("migrations.lock lists %s after %s: new lines go at the end, in order", name, last))
		}
		locked[name], last = m[1], name
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		got := files[name]
		want, ok := locked[name]
		switch {
		case !ok && name < last:
			problems = append(problems, fmt.Sprintf("%s sorts before %s, which has shipped: databases that already ran that one would run this one after it; number it after", name, last))
		case !ok:
			problems = append(problems, fmt.Sprintf("%s is not in migrations.lock; once it is final, add:\n%s  %s", name, got, name))
		case want != got:
			problems = append(problems, fmt.Sprintf("%s has changed since it shipped (lock %s, now %s): write a new migration instead", name, want[:12], got[:12]))
		}
	}
	for name := range locked {
		if _, ok := files[name]; !ok {
			problems = append(problems, fmt.Sprintf("%s is in migrations.lock but gone from migrations/: a shipped migration cannot be removed", name))
		}
	}
	return problems
}

// The lock's own rules, on made-up input. Found by an audit: a migration numbered before
// shipped ones passed once locked, malformed lines were skipped silently, and a duplicate
// line let an edited migration pass under a new checksum.
func TestCheckLockRules(t *testing.T) {
	sum := func(c byte) string { return strings.Repeat(string(c), 64) }
	files := map[string]string{"0001_a.sql": sum('a'), "0002_b.sql": sum('b')}
	good := sum('a') + "  0001_a.sql\n" + sum('b') + "  0002_b.sql\n"
	if p := checkLock(good, files); len(p) != 0 {
		t.Fatalf("a correct lock: %v", p)
	}
	cases := map[string]struct {
		lock  string
		files map[string]string
		want  string
	}{
		"edited":        {sum('a') + "  0001_a.sql\n" + sum('c') + "  0002_b.sql\n", files, "has changed"},
		"removed":       {good + sum('d') + "  0003_d.sql\n", files, "gone from migrations"},
		"not locked":    {sum('a') + "  0001_a.sql\n", files, "not in migrations.lock"},
		"out of order":  {good, map[string]string{"0001_a.sql": sum('a'), "0002_b.sql": sum('b'), "0001a_x.sql": sum('x')}, "sorts before"},
		"malformed":     {good + "deadbeef 0003_c.sql\n", files, "is not"},
		"duplicate":     {good + sum('c') + "  0002_b.sql\n", files, "twice"},
		"lines swapped": {sum('b') + "  0002_b.sql\n" + sum('a') + "  0001_a.sql\n", files, "go at the end"},
	}
	for name, c := range cases {
		p := checkLock(c.lock, c.files)
		if !strings.Contains(strings.Join(p, "\n"), c.want) {
			t.Errorf("%s: got %v, want a problem saying %q", name, p, c.want)
		}
	}
}
