package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IngestPath handles a path reported by Sonarr/Radarr (file or folder): it finds the
// library whose root contains it and ingests the file, or every video under the folder.
func (l *Library) IngestPath(ctx context.Context, path string) error {
	return l.IngestPathHinted(ctx, path, nil)
}

// IngestPathHinted is IngestPath with the season and episode(s) the arr app assigned a
// single file (see EpisodeHint). The hint is ignored for folders.
func (l *Library) IngestPathHinted(ctx context.Context, path string, hint *EpisodeHint) error {
	path = filepath.Clean(path)
	libs, err := l.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if !isUnder(lib.Root, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			// Deleted or renamed away: a rescan of the library reconciles it.
			return l.Scan(ctx, lib)
		}
		if insideSkipped(lib.Root, path) {
			return nil // a download folder, extras, .ignore'd... (same rules as a scan)
		}
		// The library's .ignore rules too, as a scan applies them: a file they leave out
		// used to be added by a webhook, then removed again by the next scan.
		ignores := ignoreCache{}
		if ignores.ignored(lib.Root, path, info.IsDir()) {
			return nil
		}
		if !info.IsDir() {
			if !videoExts[strings.ToLower(filepath.Ext(path))] || SkipFileReason(info.Name()) != "" {
				return nil
			}
			return l.addFileSafeHinted(ctx, lib, path, hint)
		}
		// A folder: every video in it. Extras after the titles they belong to (as a scan
		// does: "Film-trailer.mkv" sorts before "Film.mkv"), and one file failing does not
		// stop the others; the first error stopped the walk, so a trailer whose film was
		// not in yet kept the film out until the next scan.
		var extras []string
		var errs []error
		ingest := l.ingestOne
		if ingest == nil {
			ingest = l.addFileSafe
		}
		add := func(p string) {
			if err := ingest(ctx, lib, p); err != nil {
				errs = append(errs, err)
			}
		}
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if p != path && (SkipDirReason(p) != "" || ignores.ignored(lib.Root, p, true)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !videoExts[strings.ToLower(filepath.Ext(p))] || SkipFileReason(d.Name()) != "" || ignores.ignored(lib.Root, p, false) {
				return nil
			}
			if rel, err := filepath.Rel(lib.Root, p); err == nil {
				if _, _, _, isExtra := ExtraOf(filepath.ToSlash(rel)); isExtra {
					extras = append(extras, p)
					return nil
				}
			}
			add(p)
			return nil
		})
		if err != nil {
			return err
		}
		for _, p := range extras {
			add(p)
		}
		return errors.Join(errs...)
	}
	return fmt.Errorf("path %s is not under any library root", path)
}

// insideSkipped: path is, or sits in, a folder below root that a scan skips.
func insideSkipped(root, path string) bool {
	for p := path; isUnder(root, p) && p != root; p = filepath.Dir(p) {
		if st, err := os.Stat(p); err == nil && st.IsDir() && SkipDirReason(p) != "" {
			return true
		}
	}
	return false
}

func isUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
