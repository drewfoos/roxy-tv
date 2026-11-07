package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Extras: trailers, featurettes and the like that belong to a film or show, recognised the
// way Jellyfin, Kodi and Plex name them. They become items of type "extra" under their
// title, never titles (or versions, or episodes) of their own.

// extraFolders: a folder of extras (lower case) -> the kind of extra in it.
var extraFolders = map[string]string{
	"trailers":          "trailer",
	"featurettes":       "featurette",
	"behind the scenes": "behindthescenes",
	"deleted scenes":    "deleted",
	"interviews":        "interview",
	"scenes":            "scene",
	"shorts":            "short",
	"clips":             "clip",
	"extras":            "extra",
	"extra":             "extra",
	"other":             "extra",
	"special features":  "featurette",
	"backdrops":         "theme", // Jellyfin's theme videos
	"theme-videos":      "theme",
}

// "Film (2020)-trailer", "Film (2020).featurette2", "Show - S01E01_deleted": a suffix after
// a dash, dot or underscore, with an optional number.
var reExtraSuffix = regexp.MustCompile(`(?i)[-._](trailer|featurette|behindthescenes|deletedscene|deleted|interview|scene|clip|short|extra|other|theme)\d*$`)

var extraSuffixKind = map[string]string{"deletedscene": "deleted", "other": "extra"}

// ExtraOf says whether the video at rel (path relative to the library root, slash
// separated) is an extra: its kind, the folder of the title it belongs to (relative, ""
// for the root), and the file's name without the suffix ("Film (2020)" for
// "Film (2020)-trailer"). A file inside an extras folder belongs to the folder above it;
// one with an extra suffix belongs to its own folder.
func ExtraOf(rel string) (kind, ownerDir, base string, ok bool) {
	parts := strings.Split(rel, "/")
	for i := len(parts) - 2; i >= 1; i-- { // a folder at the root is a title, not extras
		if k, isExtra := extraFolders[strings.ToLower(parts[i])]; isExtra {
			return k, strings.Join(parts[:i], "/"), "", true
		}
	}
	name := parts[len(parts)-1]
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if m := reExtraSuffix.FindStringSubmatchIndex(stem); m != nil && m[0] > 0 {
		k := strings.ToLower(stem[m[2]:m[3]])
		if alt, ok := extraSuffixKind[k]; ok {
			k = alt
		}
		return k, strings.Join(parts[:len(parts)-1], "/"), strings.TrimSpace(stem[:m[0]]), true
	}
	return "", "", "", false
}

// extraTitle is what an extra is called: by its kind for a suffixed one ("Film-trailer2"
// is "Trailer 2": the rest is just the film's name), else its file name without release
// tags, or the kind when nothing is left.
func extraTitle(name, kind string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if m := reExtraSuffix.FindStringSubmatchIndex(stem); m != nil && m[0] > 0 {
		t := extraKindName(kind)
		if n := strings.TrimLeft(stem[m[3]:], "0"); n != "" {
			t += " " + n
		}
		return t
	}
	t := strings.TrimSpace(strings.ReplaceAll(stripBrackets(stem), "_", " "))
	if t == "" {
		t = extraKindName(kind)
	}
	return t
}

func extraKindName(kind string) string {
	switch kind {
	case "behindthescenes":
		return "Behind the scenes"
	case "deleted":
		return "Deleted scene"
	case "theme":
		return "Theme video"
	case "":
		return "Extra"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

// resolveExtra makes (or finds) the extra item for the file at path: under the title
// whose files sit in ownerDir (relative to the library root), the one named base when
// several do ("Film A (2020)-trailer" next to Film A and Film B). An error when no title
// there owns it yet or the owner is ambiguous: the scan tries again next time.
func (l *Library) resolveExtra(ctx context.Context, lib model.Library, path, kind, ownerDir, base string) (*model.Item, error) {
	dir := filepath.Join(lib.Root, filepath.FromSlash(ownerDir))
	files, err := l.store.ItemsWithMediaUnder(ctx, dir)
	if err != nil {
		return nil, err
	}
	owners := map[string]bool{}
	named := ""
	for p, id := range files {
		top, err := l.store.TopLevelID(ctx, id)
		if err != nil {
			return nil, err
		}
		owners[top] = true
		if base != "" && filepath.Dir(p) == dir && strings.EqualFold(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), base) {
			named = top
		}
	}
	owner := named
	if owner == "" && len(owners) == 1 {
		for id := range owners {
			owner = id
		}
	}
	if owner == "" {
		return nil, fmt.Errorf("extra %s: no single title in %s to belong to (%d found)", filepath.Base(path), dir, len(owners))
	}
	it := &model.Item{ID: uuid.NewString(), GUID: "extra:" + hashHex(path), LibraryID: lib.ID, Type: model.TypeExtra,
		ParentID: &owner, Title: extraTitle(filepath.Base(path), kind), ExtraType: kind}
	if err := l.store.UpsertItem(ctx, it); err != nil {
		return nil, err
	}
	return it, nil
}

// extraUpToDate reports whether an extra is already in the library as an extra and its
// file has not changed (size and time), so the scan can skip it.
func (l *Library) extraUpToDate(ctx context.Context, path string) bool {
	part, err := l.store.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		return false
	}
	it, err := l.store.ItemForPath(ctx, path)
	if err != nil || it == nil || it.Type != model.TypeExtra {
		return false // not recorded, or recorded as something else (a trailer taken for a version)
	}
	fi, err := os.Stat(path)
	return err == nil && part.Size == fi.Size() && part.ModTime.Equal(fi.ModTime().UTC().Truncate(time.Millisecond))
}

// versionLabel names a version of a film for a picker, as Jellyfin does: what its file
// name adds after the folder's name ("Film (2020) - 2160p" in "Film (2020)" is "2160p"),
// else its video height ("1080p"), else "".
func versionLabel(path string, streams []model.MediaStream) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	folder := filepath.Base(filepath.Dir(path))
	if len(stem) > len(folder) && strings.EqualFold(stem[:len(folder)], folder) {
		if l := strings.Trim(stem[len(folder):], " -._[]()"); l != "" {
			return l
		}
	}
	h := 0
	for _, st := range streams {
		if st.Type == model.StreamVideo && st.Height > h {
			h = st.Height
		}
	}
	if h > 0 {
		return fmt.Sprintf("%dp", h)
	}
	return ""
}
