package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
)

// A folder reported by a webhook: its film is added though its trailer (which sorts first
// and cannot be added before the film) comes first, and the trailer after it. Found by an
// audit: the first error stopped the walk, keeping the film out until the next scan.
func TestIngestFolderAddsTheFilmBeforeItsTrailer(t *testing.T) {
	ctx := context.Background()
	l, st, _, root, _ := moveFixture(t, model.KindMovies)
	dir := filepath.Join(root, "Film (2020)")
	writeSized(t, filepath.Join(dir, "Film (2020)-trailer.mkv"), 10)
	writeSized(t, filepath.Join(dir, "Film (2020).mkv"), 50)
	if err := l.IngestPath(ctx, dir); err != nil {
		t.Fatalf("folder ingest: %v", err)
	}
	film := itemAt(t, st, filepath.Join(dir, "Film (2020).mkv"))
	trailer := itemAt(t, st, filepath.Join(dir, "Film (2020)-trailer.mkv"))
	if film.Type != model.TypeMovie || trailer.Type != model.TypeExtra || trailer.ParentID == nil || *trailer.ParentID != film.ID {
		t.Fatalf("film %s %s, trailer %s under %v", film.Type, film.ID, trailer.Type, trailer.ParentID)
	}
}

// Webhook ingests apply the library's .ignore rules, as a scan does. Found by an audit: a
// file they leave out was added by a webhook, then removed by the next scan.
func TestIngestPathHonoursIgnoreRules(t *testing.T) {
	ctx := context.Background()
	l, st, _, root, _ := moveFixture(t, model.KindMovies)
	if err := os.WriteFile(filepath.Join(root, ".ignore"), []byte("Hidden*/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Hidden (2021)", "Hidden (2021).mkv")
	writeSized(t, file, 40)
	if err := l.IngestPath(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err := l.IngestPath(ctx, filepath.Dir(file)); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.GetPartByPath(ctx, file); p != nil {
		t.Fatal("a file the .ignore rules leave out was added by a webhook")
	}
}
