package metadata

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Local metadata: Kodi-style .nfo files and artwork sitting next to the media, the way
// Jellyfin, Kodi and Plex read them. Custom shows no provider lists (One Pace, fan edits,
// home video) carry their own: tvshow.nfo + poster.jpg in the show folder, and an .nfo
// (+ "-thumb.jpg") per episode.
//
//   <Show>/tvshow.nfo, poster.jpg|folder.jpg, fanart.jpg|backdrop.jpg, clearlogo.png|logo.png
//   <Show>/[Season NN/]<episode>.nfo, <episode>-thumb.jpg
//   <Movie>/movie.nfo or <file>.nfo, poster.jpg, fanart.jpg, clearlogo.png
//
// An .nfo that names provider ids (<uniqueid type="tmdb">, <tmdbid>, ...) only points the
// normal lookup at the right entry. One without ids is the metadata itself.

type nfoDoc struct {
	XMLName       xml.Name
	Title         string   `xml:"title"`
	OriginalTitle string   `xml:"originaltitle"`
	SortTitle     string   `xml:"sorttitle"`
	Plot          string   `xml:"plot"`
	Outline       string   `xml:"outline"`
	Tagline       string   `xml:"tagline"`
	Year          string   `xml:"year"`
	Premiered     string   `xml:"premiered"`
	Aired         string   `xml:"aired"`
	Genres        []string `xml:"genre"`
	Studios       []string `xml:"studio"`
	MPAA          string   `xml:"mpaa"`
	Rating        string   `xml:"rating"`
	Season        string   `xml:"season"`
	SeasonNumber  string   `xml:"seasonnumber"` // season.nfo
	Episode       string   `xml:"episode"`
	Status        string   `xml:"status"`
	UniqueIDs     []struct {
		Type  string `xml:"type,attr"`
		Value string `xml:",chardata"`
	} `xml:"uniqueid"`
	TMDBID    string `xml:"tmdbid"`
	TVDBID    string `xml:"tvdbid"`
	IMDBID    string `xml:"imdbid"`
	AniListID string `xml:"anilistid"`
	ID        string `xml:"id"`
}

// readNFO parses an .nfo file. Kodi allows a URL line after the XML; anything after the
// root element is ignored. A missing file returns (nil, nil).
func readNFO(path string) (*nfoDoc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var doc nfoDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &doc, nil
}

// ids collects the provider ids the .nfo names.
func (d *nfoDoc) ids() map[string]string {
	out := map[string]string{}
	for _, u := range d.UniqueIDs {
		t, v := strings.ToLower(strings.TrimSpace(u.Type)), strings.TrimSpace(u.Value)
		if v == "" {
			continue
		}
		switch t {
		case "tmdb", "tvdb", "imdb", "anilist", "mal":
			out[t] = v
		}
	}
	for k, v := range map[string]string{"tmdb": d.TMDBID, "tvdb": d.TVDBID, "imdb": d.IMDBID, "anilist": d.AniListID} {
		if v = strings.TrimSpace(v); v != "" && out[k] == "" {
			out[k] = v
		}
	}
	if v := strings.TrimSpace(d.ID); strings.HasPrefix(v, "tt") && out["imdb"] == "" {
		out["imdb"] = v
	}
	return out
}

func (d *nfoDoc) meta() store.ItemMetadata {
	m := store.ItemMetadata{
		Title:          strings.TrimSpace(d.Title),
		Overview:       strings.TrimSpace(firstNonEmpty(d.Plot, d.Outline)),
		Tagline:        strings.TrimSpace(d.Tagline),
		OfficialRating: strings.TrimSpace(d.MPAA),
		Premiered:      dateOnly(firstNonEmpty(d.Premiered, d.Aired)),
		Status:         strings.TrimSpace(d.Status),
	}
	for _, g := range d.Genres {
		if g = strings.TrimSpace(g); g != "" {
			m.Genres = append(m.Genres, g)
		}
	}
	if len(d.Studios) > 0 {
		m.Studio = strings.TrimSpace(d.Studios[0])
	}
	if r, err := strconv.ParseFloat(strings.TrimSpace(d.Rating), 64); err == nil && r > 0 && r <= 10 {
		m.CommunityRating = r
	}
	y, _ := strconv.Atoi(strings.TrimSpace(d.Year))
	if y == 0 {
		y = dateYear(m.Premiered)
	}
	if y > 0 {
		m.Year = &y
	}
	return m
}

func dateOnly(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// firstFile returns the first of names that exists in dir, as "file://<path>#<mtime>".
func firstFile(dir string, names ...string) string {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return "file://" + p + "#" + strconv.FormatInt(st.ModTime().Unix(), 10)
		}
	}
	return ""
}

// applyLocal reads an item's .nfo files and artwork. handled means the item was filled
// from them (no provider lookup); when the .nfo only carries provider ids, they are saved
// on the item (and it.ProviderIDs) and handled is false, so the lookup uses them.
func (s *Service) applyLocal(ctx context.Context, it *model.Item) (handled bool, err error) {
	paths, err := s.store.PathsUnder(ctx, it.ID)
	if err != nil || len(paths) == 0 {
		return false, err
	}
	var dir string // the show's (or movie's) folder
	var nfo *nfoDoc
	if it.Type == model.TypeMovie {
		dir = filepath.Dir(paths[0].Path)
		stem := strings.TrimSuffix(paths[0].Path, filepath.Ext(paths[0].Path))
		if nfo, err = readNFO(stem + ".nfo"); err == nil && nfo == nil {
			nfo, err = readNFO(filepath.Join(dir, "movie.nfo"))
		}
	} else {
		dir = s.showDir(ctx, it, paths[0].Path)
		if dir == "" {
			return false, nil
		}
		nfo, err = readNFO(filepath.Join(dir, "tvshow.nfo"))
	}
	if err != nil {
		slog.Warn("metadata: unreadable .nfo", "item", it.Title, "err", err)
		return false, nil
	}
	if nfo == nil {
		return false, nil
	}
	if ids := nfo.ids(); len(ids) > 0 {
		if it.ProviderIDs == nil {
			it.ProviderIDs = map[string]string{}
		}
		changed := false
		for k, v := range ids {
			if it.ProviderIDs[k] != v {
				it.ProviderIDs[k] = v
				changed = true
			}
		}
		if changed {
			if err := s.store.ReplaceProviderIDs(ctx, it.ID, it.ProviderIDs); err != nil {
				return false, err
			}
			slog.Info("metadata: ids from .nfo", "item", it.Title, "ids", ids)
		}
		return false, nil
	}

	// No ids: the .nfo and the files around it are the metadata.
	m := nfo.meta()
	if m.Title == "" {
		m.Title = it.Title
	}
	// Marks the title as described by its .nfo (Library settings lists it as matched).
	m.ProviderIDs = map[string]string{"nfo": "1"}
	if err := s.store.UpdateItemMetadata(ctx, it.ID, m); err != nil {
		return true, err
	}
	for typ, names := range map[string][]string{
		"poster":   {"poster.jpg", "poster.png", "folder.jpg", "folder.png", "cover.jpg", "cover.png"},
		"backdrop": {"fanart.jpg", "fanart.png", "backdrop.jpg", "backdrop.png", "background.jpg"},
		"logo":     {"clearlogo.png", "logo.png"},
	} {
		if src := firstFile(dir, names...); src != "" {
			s.fetchImage(ctx, it.ID, typ, src)
		}
	}
	n := 0
	if it.Type == model.TypeSeries {
		seasons := map[string]*model.Item{} // season folder -> its season item (from season.nfo)
		for _, p := range paths {
			stem := strings.TrimSuffix(p.Path, filepath.Ext(p.Path))
			ep, err := readNFO(stem + ".nfo")
			if err != nil {
				slog.Warn("metadata: unreadable episode .nfo", "path", p.Path, "err", err)
				continue
			}
			var index *int // the .nfo's <episode>: the order inside the season ("Wano 01.mp4" has no number the scanner reads)
			if ep != nil {
				if v, err := strconv.Atoi(strings.TrimSpace(ep.Episode)); err == nil && v > 0 {
					index = &v
				}
				em := ep.meta()
				em.Year = nil
				if err := s.store.UpdateItemMetadata(ctx, p.ItemID, em); err != nil {
					return true, err
				}
				n++
			}
			if src := firstFile(filepath.Dir(p.Path), filepath.Base(stem)+"-thumb.jpg", filepath.Base(stem)+"-thumb.png", filepath.Base(stem)+".jpg"); src != "" {
				s.fetchImage(ctx, p.ItemID, "thumb", src)
			}
			// A season.nfo in the file's folder places the episode in that season, whatever
			// the folder is called ("[One Pace][1-7] Romance Dawn" is season 1, "Romance Dawn").
			epDir := filepath.Dir(p.Path)
			season, seen := seasons[epDir]
			if !seen {
				season = s.localSeason(ctx, it, epDir)
				seasons[epDir] = season
			}
			if season != nil || index != nil {
				if err := s.placeInSeason(ctx, p.ItemID, season, index); err != nil {
					slog.Warn("metadata: placing episode in its season", "path", p.Path, "err", err)
				}
			}
		}
		if len(seasons) > 0 {
			if dropped, err := s.store.PruneOrphans(ctx); err == nil && dropped > 0 {
				slog.Info("metadata: empty seasons removed", "series", it.Title, "count", dropped)
			}
		}
		s.localSeasonPosters(ctx, it, dir)
	}
	slog.Info("metadata: from .nfo", "item", it.Title, "title", m.Title, "episodes", n)
	return true, nil
}

// localFileExt are the sidecar files that feed local metadata.
var localFileExt = map[string]bool{".nfo": true, ".jpg": true, ".jpeg": true, ".png": true}

// localChangedSince reports whether any .nfo or image next to the item's media (the show
// or movie folder, its season folders, beside each file) changed after t.
func (s *Service) localChangedSince(ctx context.Context, it *model.Item, t time.Time) bool {
	paths, err := s.store.PathsUnder(ctx, it.ID)
	if err != nil || len(paths) == 0 {
		return false
	}
	dirs := map[string]bool{}
	for _, p := range paths {
		dirs[filepath.Dir(p.Path)] = true
	}
	if it.Type == model.TypeSeries {
		if d := s.showDir(ctx, it, paths[0].Path); d != "" {
			dirs[d] = true
		}
	}
	for d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !localFileExt[strings.ToLower(filepath.Ext(e.Name()))] {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().After(t) {
				return true
			}
		}
	}
	return false
}

// showDir is the series folder: the first path component under the library root.
func (s *Service) showDir(ctx context.Context, it *model.Item, file string) string {
	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return ""
	}
	for _, l := range libs {
		if l.ID != it.LibraryID {
			continue
		}
		rel, err := filepath.Rel(l.Root, file)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return "" // a loose file in the root: no show folder to read
		}
		return filepath.Join(l.Root, filepath.FromSlash(parts[0]))
	}
	return ""
}

// localSeason reads <dir>/season.nfo and returns the series' season item for its
// <seasonnumber>, created (titled from the .nfo, "Romance Dawn") or retitled as needed,
// with the folder's poster.jpg. Nil when the folder has no usable season.nfo.
func (s *Service) localSeason(ctx context.Context, series *model.Item, dir string) *model.Item {
	doc, err := readNFO(filepath.Join(dir, "season.nfo"))
	if err != nil || doc == nil {
		return nil
	}
	num, err := strconv.Atoi(strings.TrimSpace(doc.SeasonNumber))
	if err != nil || num < 0 {
		return nil
	}
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = fmt.Sprintf("Season %d", num)
	}
	guid := fmt.Sprintf("%s:season:%d", series.GUID, num)
	season, err := s.store.GetItemByGUID(ctx, guid)
	if err != nil {
		return nil
	}
	if season == nil {
		season = &model.Item{ID: newID(), GUID: guid, LibraryID: series.LibraryID, Type: model.TypeSeason, ParentID: &series.ID, Title: title, IndexAired: &num}
		if err := s.store.UpsertItem(ctx, season); err != nil {
			slog.Warn("metadata: creating season from season.nfo", "dir", dir, "err", err)
			return nil
		}
	}
	if season.Title != title || (doc.Plot != "" && season.Overview != doc.Plot) {
		if err := s.store.UpdateItemMetadata(ctx, season.ID, store.ItemMetadata{Title: title, Overview: doc.Plot, ProviderIDs: map[string]string{"nfo": "1"}}); err == nil {
			season.Title = title
		}
	}
	if src := firstFile(dir, "poster.jpg", "poster.png", "folder.jpg", "folder.png"); src != "" {
		s.fetchImage(ctx, season.ID, "poster", src)
	}
	return season
}

// placeInSeason moves an episode under season when it sits elsewhere (season nil: it
// stays where it is) and gives it the episode number index when one is set.
func (s *Service) placeInSeason(ctx context.Context, episodeID string, season *model.Item, index *int) error {
	ep, err := s.store.GetItem(ctx, episodeID)
	if err != nil || ep == nil {
		return err
	}
	inSeason := season == nil || (ep.ParentID != nil && *ep.ParentID == season.ID && ep.SeasonAired != nil && season.IndexAired != nil && *ep.SeasonAired == *season.IndexAired)
	indexed := index == nil || (ep.IndexAired != nil && *ep.IndexAired == *index)
	if inSeason && indexed {
		return nil
	}
	if season != nil {
		ep.ParentID = &season.ID
		ep.SeasonAired = season.IndexAired
	}
	if index != nil {
		ep.IndexAired = index
	}
	return s.store.UpsertItem(ctx, ep)
}

// localSeasonPosters picks up "seasonNN-poster.jpg" in the show folder or "poster.jpg"
// inside "Season NN".
func (s *Service) localSeasonPosters(ctx context.Context, series *model.Item, dir string) {
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		return
	}
	for _, se := range seasons {
		if se.IndexAired == nil {
			continue
		}
		n := *se.IndexAired
		name := fmt.Sprintf("season%02d-poster", n)
		if n == 0 {
			name = "season-specials-poster"
		}
		src := firstFile(dir, name+".jpg", name+".png")
		if src == "" {
			src = firstFile(filepath.Join(dir, fmt.Sprintf("Season %02d", n)), "poster.jpg", "folder.jpg")
		}
		if src == "" {
			src = firstFile(filepath.Join(dir, fmt.Sprintf("Season %d", n)), "poster.jpg", "folder.jpg")
		}
		if src != "" {
			s.fetchImage(ctx, se.ID, "poster", src)
		}
	}
}
