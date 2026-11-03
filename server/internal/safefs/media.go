package safefs

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ErrEscapes: a media path (video, sidecar subtitle, local artwork) resolves, through a
// symlink, to somewhere outside every media folder. Roxy never reads it: a sidecar
// symlinked to /config/roxy.db would otherwise be served to any signed-in user.
var ErrEscapes = errors.New("refused: the file is outside the media folders")

// ResolveUnder resolves every symlink in path and returns the real path when it lies
// inside one of roots (each root resolved too, so a media folder that is itself a
// symlink still works). Symlinks that stay inside a root are fine.
func ResolveUnder(path string, roots ...string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if real, err = filepath.Abs(real); err != nil {
		return "", err
	}
	real = filepath.Clean(real)
	for _, root := range roots {
		if root == "" {
			continue
		}
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue // a root that is missing (unmounted disk) holds nothing
		}
		if rr, err = filepath.Abs(rr); err != nil {
			continue
		}
		if inside(real, filepath.Clean(rr)) {
			return real, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrEscapes, path)
}

// MediaPath is ResolveUnder against the media folders given to SetMediaRoots (configured
// roots plus every library). With no media folders known, nothing is allowed.
func MediaPath(path string) (string, error) {
	mu.RLock()
	roots := append([]string(nil), mediaDirs...)
	mu.RUnlock()
	return ResolveUnder(path, roots...)
}

// NoEscape allows a path that involves no symlink at all (the scanner found it by walking
// a media folder, so it is inside one by construction) and otherwise requires the real
// path to be inside a media folder. Used where a path is read on the strength of having
// been found next to media: .nfo files and local artwork.
func NoEscape(path string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if realAbs, err := filepath.Abs(real); err == nil && filepath.Clean(realAbs) == filepath.Clean(abs) {
		return nil
	}
	_, err = MediaPath(path)
	return err
}
