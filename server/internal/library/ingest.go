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
		if !info.IsDir() {
			if !videoExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			return l.addFileSafe(ctx, lib, path)
		}
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !videoExts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			return l.addFileSafe(ctx, lib, p)
		})
	}
	return fmt.Errorf("path %s is not under any library root", path)
}

func isUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
