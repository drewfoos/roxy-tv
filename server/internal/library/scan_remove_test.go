package library

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// recordedLibrary makes a library with two folders of two files each, recorded in the
// store exactly as on disk (size and mtime), so a scan finds them unchanged and never
// probes. It returns the library, the store, and the file paths.
func recordedLibrary(t *testing.T) (*Library, *store.Store, model.Library, []string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	root := t.TempDir()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: root}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, dir := range []string{"A", "B"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"one.mkv", "two.mkv"} {
			path := filepath.Join(root, dir, name)
			if err := os.WriteFile(path, []byte("not really a video"), 0o644); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			item := &model.Item{ID: uuid.NewString(), GUID: "test:" + dir + "/" + name, LibraryID: lib.ID, Type: model.TypeMovie, Title: dir + " " + name}
			if err := st.UpsertItem(ctx, item); err != nil {
				t.Fatal(err)
			}
			src := &model.MediaSource{ID: uuid.NewString(), ItemID: item.ID, Container: "mkv", Size: fi.Size(), IsPrimary: true,
				Parts: []model.MediaPart{{ID: uuid.NewString(), Path: path, Size: fi.Size(), ModTime: fi.ModTime()}}}
			if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, path)
		}
	}
	return New(st, nil, nil), st, *lib, paths
}

func recorded(t *testing.T, st *store.Store, lib model.Library) []string {
	t.Helper()
	m, err := st.ListPartPaths(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func wantRecorded(t *testing.T, st *store.Store, lib model.Library, want []string) {
	t.Helper()
	want = append([]string(nil), want...)
	sort.Strings(want)
	if got := recorded(t, st, lib); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("recorded files:\n got  %v\n want %v", got, want)
	}
}

// A file really deleted from a readable folder is still removed: the guards below must
// not stop ordinary cleanup.
func TestScanRemovesDeletedFile(t *testing.T) {
	l, st, lib, paths := recordedLibrary(t)
	if err := os.Remove(paths[3]); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	wantRecorded(t, st, lib, paths[:3])
}

// The library folder itself is gone: nothing is removed, and the scan fails saying so.
func TestScanKeepsRecordsWhenRootIsUnreadable(t *testing.T) {
	l, st, lib, paths := recordedLibrary(t)
	if err := os.RemoveAll(lib.Root); err != nil {
		t.Fatal(err)
	}
	err := l.Scan(context.Background(), lib)
	if err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("scan error: %v", err)
	}
	wantRecorded(t, st, lib, paths)
}

// The folder is there but holds no media at all while files are recorded (the empty
// folder Docker leaves for a drive that did not mount): nothing is removed.
func TestScanKeepsRecordsWhenRootIsEmpty(t *testing.T) {
	l, st, lib, paths := recordedLibrary(t)
	for _, dir := range []string{"A", "B"} {
		if err := os.RemoveAll(filepath.Join(lib.Root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	err := l.Scan(context.Background(), lib)
	if err == nil || !strings.Contains(err.Error(), "drive mounted") {
		t.Fatalf("scan error: %v", err)
	}
	wantRecorded(t, st, lib, paths)
}

// A subfolder that cannot be read keeps what was recorded under it; a file really
// deleted elsewhere is still removed; the scan reports the folder.
func TestScanKeepsRecordsInUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads folders regardless of their permissions")
	}
	l, st, lib, paths := recordedLibrary(t)
	locked := filepath.Join(lib.Root, "A")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if err := os.Remove(paths[3]); err != nil { // B/two.mkv really deleted
		t.Fatal(err)
	}
	err := l.Scan(context.Background(), lib)
	if err == nil || !strings.Contains(err.Error(), "could not be read") || !strings.Contains(err.Error(), locked) {
		t.Fatalf("scan error: %v", err)
	}
	wantRecorded(t, st, lib, paths[:3]) // A/one, A/two kept; B/one kept; B/two removed
}
