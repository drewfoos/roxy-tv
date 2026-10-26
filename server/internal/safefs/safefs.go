// Package safefs is the only way the server deletes, renames or writes files. Every call
// checks the path is inside Roxy's own data folder and not inside a media folder, so
// whatever happens in the code above, Roxy never changes or removes anyone's media.
// Media folders are read-only to Roxy, always.
package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu        sync.RWMutex
	dataDir   string
	mediaDirs []string
)

// ErrOutside: a write or delete aimed outside the data folder (or into media) was refused.
var ErrOutside = errors.New("refused: Roxy only writes inside its own data folder")

// Init sets the one folder Roxy may write in. Called once at start.
func Init(data string) {
	abs, err := filepath.Abs(data)
	if err != nil {
		abs = data
	}
	mu.Lock()
	dataDir = filepath.Clean(abs)
	mu.Unlock()
}

// SetMediaRoots records the media folders, which are never written to even if one
// happens to sit inside the data folder.
func SetMediaRoots(roots []string) {
	clean := make([]string, 0, len(roots))
	for _, r := range roots {
		if abs, err := filepath.Abs(r); err == nil {
			clean = append(clean, filepath.Clean(abs))
		}
	}
	mu.Lock()
	mediaDirs = clean
	mu.Unlock()
}

// Check returns nil when path may be written or deleted.
func Check(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrOutside, path)
	}
	abs = filepath.Clean(abs)
	mu.RLock()
	defer mu.RUnlock()
	if dataDir == "" || !inside(abs, dataDir) || abs == dataDir {
		return fmt.Errorf("%w: %s", ErrOutside, path)
	}
	for _, m := range mediaDirs {
		if abs == m || inside(abs, m) || inside(m, abs) {
			return fmt.Errorf("%w (media folder): %s", ErrOutside, path)
		}
	}
	return nil
}

func inside(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func Remove(path string) error {
	if err := Check(path); err != nil {
		return err
	}
	return os.Remove(path)
}

func RemoveAll(path string) error {
	if err := Check(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func Rename(from, to string) error {
	if err := Check(from); err != nil {
		return err
	}
	if err := Check(to); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := Check(path); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

func MkdirAll(path string, perm os.FileMode) error {
	if err := Check(path); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}
