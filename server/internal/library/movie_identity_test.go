package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func touch(t *testing.T, root string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		p := filepath.Join(root, r)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func identity(t *testing.T, root, rel string) movieID {
	t.Helper()
	m, err := movieIdentity(root, filepath.Join(root, rel), "", "")
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return m
}

// Two different films in one folder are two items, each with its own title and year.
// Identity used to be the folder alone, so both became one item called after the folder.
func TestMovieIdentityCollectionFolder(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Studio Collection/Movie A (2022).mkv", "Studio Collection/Movie B (2023).mkv")
	a, b := identity(t, root, "Studio Collection/Movie A (2022).mkv"), identity(t, root, "Studio Collection/Movie B (2023).mkv")
	if a.guid == b.guid {
		t.Fatalf("two films share one item: %s", a.guid)
	}
	if a.title != "Movie A" || a.year == nil || *a.year != 2022 || b.title != "Movie B" || b.year == nil || *b.year != 2023 {
		t.Fatalf("titles: %q %v / %q %v", a.title, a.year, b.title, b.year)
	}
	// Versions of one film inside a collection still share its item.
	touch(t, root, "Studio Collection/Movie A (2022) - 2160p.mkv")
	if v := identity(t, root, "Studio Collection/Movie A (2022) - 2160p.mkv"); v.guid != identity(t, root, "Studio Collection/Movie A (2022).mkv").guid {
		t.Fatal("two versions of Movie A in a collection were split")
	}
}

// One film with several versions in its own folder stays one item named after the folder
// (the live library's Chainsaw Man 1080p / 2160p), and a lone file keeps the folder's
// identity whatever it is called.
func TestMovieIdentityOneFilmFolder(t *testing.T) {
	root := t.TempDir()
	touch(t, root,
		"Reze Arc (2025)/Reze Arc (2025) - 1080p.mkv", "Reze Arc (2025)/Reze Arc (2025) - 2160p.mkv",
		"Lone (2020)/lone-1080p-x264.mkv",
		"Scene/Movie.C.2019.1080p.WEB.x264-GRP.mkv", "Scene/Movie.C.2019.2160p.WEB.x265-GRP.mkv",
	)
	a, b := identity(t, root, "Reze Arc (2025)/Reze Arc (2025) - 1080p.mkv"), identity(t, root, "Reze Arc (2025)/Reze Arc (2025) - 2160p.mkv")
	if a.guid != b.guid || a.title != "Reze Arc" || a.guid != guidFor(nil, "movie", root, "Reze Arc (2025)") {
		t.Fatalf("versions: %s %s %q", a.guid, b.guid, a.title)
	}
	if l := identity(t, root, "Lone (2020)/lone-1080p-x264.mkv"); l.title != "Lone" || l.guid != guidFor(nil, "movie", root, "Lone (2020)") {
		t.Fatalf("lone file: %+v", l)
	}
	// Named differently from the folder, but the same film: still one item.
	if c1, c2 := identity(t, root, "Scene/Movie.C.2019.1080p.WEB.x264-GRP.mkv"), identity(t, root, "Scene/Movie.C.2019.2160p.WEB.x265-GRP.mkv"); c1.guid != c2.guid {
		t.Fatal("two scene releases of one film were split")
	}
}

// Provider ids: films in a collection folder with their own ids are told apart by them;
// a one-film folder's ids win, and the file's fill in what the folder lacks.
func TestMovieIdentityProviderIDs(t *testing.T) {
	root := t.TempDir()
	touch(t, root,
		"Box/Film One (2001) {tmdb-101}.mkv", "Box/Film Two (2002) {tmdb-202}.mkv",
		"Film Three (2003) {tmdb-303}/Film Three (2003).mkv",
		"Film Four (2004)/Film Four (2004) {imdb-tt0000404}.mkv",
	)
	one, two := identity(t, root, "Box/Film One (2001) {tmdb-101}.mkv"), identity(t, root, "Box/Film Two (2002) {tmdb-202}.mkv")
	if one.guid != "tmdb:movie:101" || two.guid != "tmdb:movie:202" {
		t.Fatalf("collection ids: %s %s", one.guid, two.guid)
	}
	if three := identity(t, root, "Film Three (2003) {tmdb-303}/Film Three (2003).mkv"); three.guid != "tmdb:movie:303" {
		t.Fatalf("folder id: %s", three.guid)
	}
	if four := identity(t, root, "Film Four (2004)/Film Four (2004) {imdb-tt0000404}.mkv"); four.guid != "imdb:movie:tt0000404" || four.ids["imdb"] != "tt0000404" {
		t.Fatalf("file id under a folder without one: %+v", four)
	}
}

// A file in the library root is its own movie, as before; a folder that cannot be listed
// is an error, not a guess that could move the file to another item.
func TestMovieIdentityRootAndUnreadable(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Solo (2018).mkv", "Box/X (2001).mkv", "Box/Y (2002).mkv")
	if s := identity(t, root, "Solo (2018).mkv"); s.guid != guidFor(nil, "movie", root, "Solo (2018).mkv") || s.title != "Solo" {
		t.Fatalf("root file: %+v", s)
	}
	readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("input/output error") }
	t.Cleanup(func() { readDir = os.ReadDir })
	if _, err := movieIdentity(root, filepath.Join(root, "Box/X (2001).mkv"), "", ""); err == nil {
		t.Fatal("an unreadable folder must be an error")
	}
}

// A film alone in a folder, watched, then joined by a second film: re-ingesting it keeps
// its item (and so its watch history), and the newcomer gets its own. The reverse (a
// collection that shrinks to one film) keeps it as well. The layout-based reading alone
// moved the first film to a new item, its history left on the old one.
func TestMovieKeepsItsItemWhenTheFolderChanges(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := t.TempDir()
	lib := &model.Library{Name: "Movies", Kind: model.KindMovies, Root: root}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	l := New(st, nil, nil)
	ingest := func(rel string) *model.Item {
		t.Helper()
		path := filepath.Join(root, rel)
		it, err := l.resolveItem(ctx, *lib, path)
		if err != nil {
			t.Fatal(err)
		}
		src := &model.MediaSource{ID: stableID("source", path), ItemID: it.ID, IsPrimary: true,
			Parts: []model.MediaPart{{ID: stableID("part", path), Path: path}}}
		if err := st.ReplaceSource(ctx, src, "{}"); err != nil {
			t.Fatal(err)
		}
		return it
	}

	touch(t, root, "Collection/Movie A (2022).mkv")
	a := ingest("Collection/Movie A (2022).mkv")
	scope, err := st.DefaultProfileID(ctx, mustUser(t, st))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgress(ctx, scope, a.GUID, 600_000, nil, nil); err != nil {
		t.Fatal(err)
	}

	touch(t, root, "Collection/Movie B (2023).mkv")
	b := ingest("Collection/Movie B (2023).mkv")
	a2 := ingest("Collection/Movie A (2022).mkv") // a forced scan, a changed file, a new subtitle
	if a2.GUID != a.GUID || a2.ID != a.ID {
		t.Fatalf("Movie A moved from %s to %s when Movie B joined it", a.GUID, a2.GUID)
	}
	if b.GUID == a.GUID {
		t.Fatal("Movie B was put on Movie A's item")
	}
	if a2.Title != "Movie A" {
		t.Fatalf("Movie A's name should follow the folder as it is now: %q", a2.Title)
	}
	if d, err := st.GetUserItemData(ctx, scope, a2.GUID); err != nil || d == nil || d.PositionMs != 600_000 {
		t.Fatalf("watch history: %+v %v", d, err)
	}

	// The reverse: Movie A leaves, Movie B is alone, and keeps its item.
	if err := os.Remove(filepath.Join(root, "Collection/Movie A (2022).mkv")); err != nil {
		t.Fatal(err)
	}
	if b2 := ingest("Collection/Movie B (2023).mkv"); b2.GUID != b.GUID {
		t.Fatalf("Movie B moved from %s to %s when it was left alone", b.GUID, b2.GUID)
	}
	// A new file still gets the layout's reading: alone in a new folder, the folder's.
	touch(t, root, "Solo (2020)/solo.mkv")
	if s := ingest("Solo (2020)/solo.mkv"); s.GUID != guidFor(nil, "movie", root, "Solo (2020)") {
		t.Fatalf("new file: %s", s.GUID)
	}
}

func mustUser(t *testing.T, st *store.Store) string {
	t.Helper()
	u := &model.User{ID: "u1", Name: "drew"}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}
