package library

import (
	"context"
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
		if !info.IsDir() {
			if !videoExts[strings.ToLower(filepath.Ext(path))] || SkipFileReason(info.Name()) != "" {
				return nil
			}
			return l.addFileSafeHinted(ctx, lib, path, hint)
		}
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if p != path && SkipDirReason(p) != "" {
					return filepath.SkipDir
				}
				return nil
			}
			if !videoExts[strings.ToLower(filepath.Ext(p))] || SkipFileReason(d.Name()) != "" {
				return nil
			}
			return l.addFileSafe(ctx, lib, p)
		})
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
