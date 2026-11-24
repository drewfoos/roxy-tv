package music

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	// The formats a cover comes in: without these here, a picture is only recognised when
	// some other package happens to have loaded its decoder.
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/imaging"
	"github.com/roxy-media/roxy/server/internal/safefs"
)

// maxArtBytes: a cover is at most a few megabytes; a larger "picture" is not kept.
const maxArtBytes = 24 << 20

// ArtStore keeps cover pictures in the cache folder, each under the hash of its bytes.
// An album's forty tracks carry the same picture forty times: it is kept once, and a
// picture's address never changes what it shows, so clients may keep it for good.
type ArtStore struct {
	dir string
}

// NewArtStore keeps pictures under dir (inside Roxy's data folder).
func NewArtStore(dir string) *ArtStore { return &ArtStore{dir: dir} }

// Dir is the folder the pictures are kept in.
func (a *ArtStore) Dir() string { return a.dir }

// ValidArtHash reports whether s is a hash an ArtStore hands out (it is used as a file
// name, so nothing else may be passed on).
func ValidArtHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Path is where the picture with this hash is kept.
func (a *ArtStore) Path(hash string) string {
	return filepath.Join(a.dir, hash[:2], hash)
}

// Has reports whether the picture with this hash is in the store.
func (a *ArtStore) Has(hash string) bool {
	if !ValidArtHash(hash) {
		return false
	}
	_, err := os.Stat(a.Path(hash))
	return err == nil
}

// ErrNotAPicture: the bytes are not an image Roxy can show (or one too large to decode).
var ErrNotAPicture = errors.New("not a picture")

// Put keeps a picture and returns its hash. Bytes that are not an image are refused.
func (a *ArtStore) Put(data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxArtBytes {
		return "", ErrNotAPicture
	}
	if _, _, err := imaging.CheckConfig(data); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotAPicture, err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:20])
	path := a.Path(hash)
	if _, err := os.Stat(path); err == nil {
		return hash, nil
	}
	if err := safefs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := safefs.WriteFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}
	return hash, nil
}

// Sweep removes the pictures (and their resized copies) that keep says nothing uses any
// more, and returns how many files went. A store whose pictures are mostly unused is left
// alone and reported: that is what a library whose drive is not mounted looks like.
func (a *ArtStore) Sweep(keep map[string]bool) (removed int, err error) {
	type file struct{ path, hash string }
	var all, unused []file
	shards, err := os.ReadDir(a.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	hashes := map[string]bool{}
	for _, sh := range shards {
		if !sh.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(a.dir, sh.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			hash := name
			if i := strings.IndexByte(name, '.'); i >= 0 {
				hash = name[:i] // a resized copy: "<hash>.w320.jpg"
			}
			if !ValidArtHash(hash) {
				continue
			}
			f := file{filepath.Join(a.dir, sh.Name(), name), hash}
			all = append(all, f)
			hashes[hash] = true
			if !keep[hash] {
				unused = append(unused, f)
			}
		}
	}
	gone := 0
	for h := range hashes {
		if !keep[h] {
			gone++
		}
	}
	if len(hashes) >= 20 && gone*2 > len(hashes) {
		return 0, fmt.Errorf("%d of %d pictures look unused: left alone", gone, len(hashes))
	}
	for _, f := range unused {
		if safefs.Remove(f.path) == nil {
			removed++
		}
	}
	return removed, nil
}

// EmbeddedPicture copies the cover picture out of an audio file (stream is the index of
// the picture stream ffprobe reported).
func EmbeddedPicture(ctx context.Context, ff *ffmpeg.FFmpeg, path string, stream int) ([]byte, error) {
	out, err := ff.Run(ctx, "", "-v", "error", "-i", path, "-map", "0:"+strconv.Itoa(stream), "-frames:v", "1", "-c:v", "copy", "-f", "image2pipe", "-")
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotAPicture
	}
	return out, nil
}

// folderArtNames are the pictures a folder may hold of the album in it, best first.
var folderArtNames = []string{"cover", "folder", "front", "album"}

var artExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// pickFolderArt chooses a folder's album picture among its file names: "cover.jpg" before
// "folder.jpg" before "front.png" ... ("" for none). base names an artist's own picture
// instead when it is "artist".
func pickFolderArt(names []string, bases ...string) string {
	if len(bases) == 0 {
		bases = folderArtNames
	}
	for _, base := range bases {
		for _, n := range names {
			ext := strings.ToLower(filepath.Ext(n))
			if artExts[ext] && strings.EqualFold(strings.TrimSuffix(n, filepath.Ext(n)), base) {
				return n
			}
		}
	}
	return ""
}

// readPicture reads a picture file from a music folder, refusing one that is too large.
func readPicture(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() || fi.Size() > maxArtBytes {
		return nil, ErrNotAPicture
	}
	return os.ReadFile(path)
}
