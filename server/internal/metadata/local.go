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
	"github.com/roxy-media/roxy/server/internal/safefs"
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
	// <episodenumberend>: the last episode a multi-episode file holds (Jellyfin's tag).
	EpisodeNumberEnd string `xml:"episodenumberend"`
	// <namedseason number="1">Arc name</namedseason> in tvshow.nfo: season titles.
	NamedSeasons []struct {
		Number string `xml:"number,attr"`
		Value  string `xml:",chardata"`
	} `xml:"namedseason"`
	// <lockdata>true</lockdata>: the .nfo is the metadata, even when it names ids (no
	// online lookup changes it).
	LockData string `xml:"lockdata"`
	// A special's place in story order (Jellyfin / TVDB tags, then Kodi's).
	AirsBeforeSeason  string `xml:"airsbefore_season"`
	AirsBeforeEpisode string `xml:"airsbefore_episode"`
	AirsAfterSeason   string `xml:"airsafter_season"`
	DisplaySeason     string `xml:"displayseason"`
	DisplayEpisode    string `xml:"displayepisode"`
	UniqueIDs         []struct {
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
	if err := safefs.NoEscape(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err // symlinked out of the media folders: not read
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	docs, err := decodeNFO(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return docs[0], nil
}

// readNFOAll is readNFO for a multi-episode file's .nfo, which holds one <episodedetails>
// block per episode one after the other: all of them, in order (nil for no file).
func readNFOAll(path string) ([]*nfoDoc, error) {
	if err := safefs.NoEscape(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	docs, err := decodeNFO(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return docs, nil
}

// decodeNFO reads the root elements of an .nfo in order; anything that is not XML after
// them (Kodi's trailing URL line) ends the list. At least one, or an error.
func decodeNFO(b []byte) ([]*nfoDoc, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var docs []*nfoDoc
	for {
		var doc nfoDoc
		if err := dec.Decode(&doc); err != nil {
			if len(docs) > 0 {
				return docs, nil
			}
			return nil, err
		}
		docs = append(docs, &doc)
	}
}

// placement is where a special belongs in story order: airsbefore_season /
// airsbefore_episode / airsafter_season, or else Kodi's displayseason / displayepisode
// (an episode of 4096, Kodi's "after the season", or none means after the season).
// All nil when the .nfo places it nowhere.
func (d *nfoDoc) placement() (beforeSeason, beforeEp, afterSeason *int) {
	num := func(v string) *int {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return &n
		}
		return nil
	}
	beforeSeason, beforeEp, afterSeason = num(d.AirsBeforeSeason), num(d.AirsBeforeEpisode), num(d.AirsAfterSeason)
	if beforeSeason != nil {
		return beforeSeason, beforeEp, nil // airsbefore wins: it is the more precise place
	}
	if afterSeason != nil {
		return nil, nil, afterSeason
	}
	if ds := num(d.DisplaySeason); ds != nil {
		if de := num(d.DisplayEpisode); de != nil && *de < 4096 {
			return ds, de, nil
		}
		return nil, nil, ds
	}
	return nil, nil, nil
}

// locked reports <lockdata>true</lockdata>.
func (d *nfoDoc) locked() bool { return strings.EqualFold(strings.TrimSpace(d.LockData), "true") }

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
	sortTitle := strings.TrimSpace(d.SortTitle) // "" clears one set before: the .nfo decides
	m := store.ItemMetadata{
		Title:          strings.TrimSpace(d.Title),
		SortTitle:      &sortTitle,
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

// imageExts are the picture files local artwork may be (Kodi and Jellyfin names).
var imageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".tbn"}

// withExts expands base names ("poster") to every artwork extension.
func withExts(bases ...string) []string {
	out := make([]string, 0, len(bases)*len(imageExts))
	for _, b := range bases {
		for _, e := range imageExts {
			out = append(out, b+e)
		}
	}
	return out
}

// firstFile returns the first of names that exists in dir, as "file://<path>#<mtime>".
// Names are matched without regard to case ("Poster.JPG").
func firstFile(dir string, names ...string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	have := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			have[strings.ToLower(e.Name())] = e.Name()
		}
	}
	for _, n := range names {
		real, ok := have[strings.ToLower(n)]
		if !ok {
			continue
		}
		p := filepath.Join(dir, real)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && safefs.NoEscape(p) == nil {
			return "file://" + p + "#" + strconv.FormatInt(st.ModTime().Unix(), 10)
		}
	}
	return ""
}

// applyLocal reads an item's .nfo files and artwork before the provider lookup. handled
// means the item was filled from them (no provider lookup): an .nfo without provider
// ids, or one with <lockdata>true</lockdata>. An .nfo with ids has them saved on the item
// (and it.ProviderIDs) so the lookup uses them, and handled is false; overlayLocal then
// lays the .nfo's own fields, episode .nfo files and local artwork over what the lookup
// brought, as Jellyfin does.
func (s *Service) applyLocal(ctx context.Context, it *model.Item) (handled bool, err error) {
	loc, err := s.localFor(ctx, it)
	if err != nil || loc == nil || loc.nfo == nil {
		return false, err
	}
	nfo := loc.nfo
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
		if !nfo.locked() {
			return false, nil
		}
	}

	// No ids (or locked): the .nfo and the files around it are the metadata.
	m := nfo.meta()
	if m.Title == "" {
		m.Title = it.Title
	}
	// Marks the title as described by its .nfo (Library settings lists it as matched).
	m.ProviderIDs = map[string]string{"nfo": "1"}
	if err := s.store.UpdateItemMetadata(ctx, it.ID, m); err != nil {
		return true, err
	}
	n, err := s.localRest(ctx, it, loc)
	if err != nil {
		return true, err
	}
	slog.Info("metadata: from .nfo", "item", it.Title, "title", m.Title, "episodes", n)
	return true, nil
}

// overlayLocal runs after the provider lookup for an item applyLocal did not handle: the
// fields its .nfo sets win over the provider's (an empty field leaves the provider's),
// season names from tvshow.nfo, every episode's .nfo, and artwork next to the media wins
// over fetched art (art picked by hand stays). Nothing is read when there is nothing
// local.
func (s *Service) overlayLocal(ctx context.Context, it *model.Item) {
	loc, err := s.localFor(ctx, it)
	if err != nil {
		slog.Warn("metadata: local files", "item", it.Title, "err", err)
		return
	}
	if loc == nil {
		return
	}
	if loc.nfo != nil {
		m := nfoOverlay(loc.nfo.meta())
		if err := s.store.UpdateItemMetadata(ctx, it.ID, m); err != nil {
			slog.Warn("metadata: .nfo fields", "item", it.Title, "err", err)
		}
	}
	if n, err := s.localRest(ctx, it, loc); err != nil {
		slog.Warn("metadata: local files", "item", it.Title, "err", err)
	} else if n > 0 {
		slog.Info("metadata: episode .nfo files", "item", it.Title, "episodes", n)
	}
}

// nfoOverlay keeps only what an .nfo with ids should lay over a provider's data: its
// genres and studio only when it names some (an empty list would not clear anything
// anyway), its title only when set. ItemMetadata's update already skips empty fields.
func nfoOverlay(m store.ItemMetadata) store.ItemMetadata {
	m.ProviderIDs = nil
	return m
}

// local is what sits next to an item's media: its folder, its first file's stem, and its
// top .nfo (movie.nfo / <file>.nfo, or tvshow.nfo).
type local struct {
	dir, stem string
	paths     []store.ItemPath
	nfo       *nfoDoc
}

func (s *Service) localFor(ctx context.Context, it *model.Item) (*local, error) {
	paths, err := s.store.PathsUnder(ctx, it.ID)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	loc := &local{paths: paths}
	if it.Type == model.TypeMovie {
		loc.dir = filepath.Dir(paths[0].Path)
		loc.stem = strings.TrimSuffix(filepath.Base(paths[0].Path), filepath.Ext(paths[0].Path))
		if loc.nfo, err = readNFO(filepath.Join(loc.dir, loc.stem+".nfo")); err == nil && loc.nfo == nil {
			loc.nfo, err = readNFO(filepath.Join(loc.dir, "movie.nfo"))
		}
	} else {
		loc.dir = s.showDir(ctx, it, paths[0].Path)
		if loc.dir == "" {
			return nil, nil
		}
		loc.nfo, err = readNFO(filepath.Join(loc.dir, "tvshow.nfo"))
	}
	if err != nil {
		slog.Warn("metadata: unreadable .nfo", "item", it.Title, "err", err)
		loc.nfo = nil
	}
	return loc, nil
}

// localRest applies everything local but the top .nfo's fields: artwork, and for a
// series the season names, every episode's .nfo and thumb, and season posters. Returns
// how many episode .nfo files were read.
func (s *Service) localRest(ctx context.Context, it *model.Item, loc *local) (int, error) {
	s.localArtwork(ctx, it, loc)
	if it.Type != model.TypeSeries {
		return 0, nil
	}
	if loc.nfo != nil {
		for _, ns := range loc.nfo.NamedSeasons {
			num, err := strconv.Atoi(strings.TrimSpace(ns.Number))
			title := strings.TrimSpace(ns.Value)
			if err != nil || num < 0 || title == "" {
				continue
			}
			if se := s.seasonByNumber(ctx, it, num, ""); se != nil && se.Title != title {
				if err := s.store.UpdateItemMetadata(ctx, se.ID, store.ItemMetadata{Title: title}); err != nil {
					slog.Warn("metadata: season name from .nfo", "series", it.Title, "season", num, "err", err)
				}
			}
		}
	}
	n := 0
	seasons := map[string]*model.Item{} // season folder -> its season item (from season.nfo)
	moved := false
	for _, p := range loc.paths {
		stem := strings.TrimSuffix(p.Path, filepath.Ext(p.Path))
		docs, err := readNFOAll(stem + ".nfo")
		if err != nil {
			slog.Warn("metadata: unreadable episode .nfo", "path", p.Path, "err", err)
			continue
		}
		var index, end *int // the .nfo's <episode> (and the last one a multi-episode file holds)
		var seasonNum *int  // the .nfo's <season>
		if len(docs) > 0 {
			ep := docs[0]
			if v, err := strconv.Atoi(strings.TrimSpace(ep.Episode)); err == nil && v > 0 {
				index = &v
			}
			if v, err := strconv.Atoi(strings.TrimSpace(ep.Season)); err == nil && v >= 0 {
				seasonNum = &v
			}
			last := strings.TrimSpace(ep.EpisodeNumberEnd)
			if len(docs) > 1 {
				last = strings.TrimSpace(docs[len(docs)-1].Episode)
			}
			if v, err := strconv.Atoi(last); err == nil && index != nil && v > *index {
				end = &v
			}
			em := ep.meta()
			em.Year = nil
			if len(docs) > 1 { // "Title A / Title B", as Jellyfin shows a multi-episode file
				var titles []string
				for _, d := range docs {
					if t := strings.TrimSpace(d.Title); t != "" {
						titles = append(titles, t)
					}
				}
				em.Title = strings.Join(titles, " / ")
			}
			if err := s.store.UpdateItemMetadata(ctx, p.ItemID, em); err != nil {
				return n, err
			}
			if ids := ep.ids(); len(ids) > 0 {
				if err := s.store.SetProviderIDs(ctx, p.ItemID, ids); err != nil {
					return n, err
				}
			}
			// A special's place in story order (next-up plays it there). Only season 0
			// has one; the .nfo decides, so one it no longer names is cleared.
			bs, be, as := ep.placement()
			if seasonNum == nil || *seasonNum != 0 {
				bs, be, as = nil, nil, nil
			}
			if err := s.store.SetSpecialPlacement(ctx, p.ItemID, bs, be, as); err != nil {
				return n, err
			}
			n++
		}
		base := filepath.Base(stem)
		if src := firstFile(filepath.Dir(p.Path), append(withExts(base+"-thumb"), withExts(base)...)...); src != "" {
			s.fetchImage(ctx, p.ItemID, "thumb", src)
		}
		// Where the episode belongs: a season.nfo in its folder places it in that season,
		// whatever the folder is called ("[One Pace][1-7] Romance Dawn" is season 1);
		// otherwise the .nfo's own <season>.
		epDir := filepath.Dir(p.Path)
		season, seen := seasons[epDir]
		if !seen {
			season = s.localSeason(ctx, it, epDir)
			seasons[epDir] = season
			if season != nil {
				moved = true
			}
		}
		if season == nil && seasonNum != nil {
			season = s.seasonByNumber(ctx, it, *seasonNum, "")
			moved = true
		}
		if season != nil || index != nil {
			if err := s.placeInSeason(ctx, p.ItemID, season, index, end); err != nil {
				slog.Warn("metadata: placing episode in its season", "path", p.Path, "err", err)
			}
		}
	}
	if moved {
		if dropped, err := s.store.PruneOrphans(ctx); err == nil && dropped > 0 {
			slog.Info("metadata: empty seasons removed", "series", it.Title, "count", dropped)
		}
	}
	s.localSeasonPosters(ctx, it, loc.dir)
	return n, nil
}

// localArtwork picks up artwork next to the media under the names Kodi and Jellyfin
// use, in any case and as .jpg, .jpeg, .png, .webp or .tbn. It wins over fetched art;
// art picked by hand (locked) stays.
func (s *Service) localArtwork(ctx context.Context, it *model.Item, loc *local) {
	poster := []string{"poster", "folder", "cover", "default"}
	if it.Type == model.TypeMovie {
		poster = append([]string{loc.stem + "-poster"}, append(poster, "movie")...)
	} else {
		poster = append(poster, "show")
	}
	backdrop := []string{"fanart", "backdrop", "background", "art", "fanart1", "backdrop1"}
	logo := []string{"clearlogo", "logo"}
	if it.Type == model.TypeMovie {
		backdrop = append([]string{loc.stem + "-fanart", loc.stem + "-backdrop"}, backdrop...)
		logo = append([]string{loc.stem + "-clearlogo", loc.stem + "-logo"}, logo...)
	}
	for typ, bases := range map[string][]string{"poster": poster, "backdrop": backdrop, "logo": logo} {
		if src := firstFile(loc.dir, withExts(bases...)...); src != "" {
			s.fetchImage(ctx, it.ID, typ, src)
		}
	}
}

// localFileExt are the sidecar files that feed local metadata.
var localFileExt = map[string]bool{".nfo": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".tbn": true}

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
// with the folder's poster. Nil when the folder has no usable season.nfo.
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
	season := s.seasonByNumber(ctx, series, num, title)
	if season == nil {
		return nil
	}
	if (title != "" && season.Title != title) || (doc.Plot != "" && season.Overview != doc.Plot) {
		if title == "" {
			title = season.Title
		}
		if err := s.store.UpdateItemMetadata(ctx, season.ID, store.ItemMetadata{Title: title, Overview: doc.Plot, ProviderIDs: map[string]string{"nfo": "1"}}); err == nil {
			season.Title = title
		}
	}
	if src := firstFile(dir, withExts("poster", "folder")...); src != "" {
		s.fetchImage(ctx, season.ID, "poster", src)
	}
	return season
}

// seasonByNumber is the series' season item for num, created when missing (titled title,
// or "Season N" / "Specials"). Its GUID is the one the scanner gives the same season.
func (s *Service) seasonByNumber(ctx context.Context, series *model.Item, num int, title string) *model.Item {
	guid := fmt.Sprintf("%s:season:%d", series.GUID, num)
	season, err := s.store.GetItemByGUID(ctx, guid)
	if err != nil {
		return nil
	}
	if season != nil {
		return season
	}
	if title == "" {
		title = fmt.Sprintf("Season %d", num)
		if num == 0 {
			title = "Specials"
		}
	}
	season = &model.Item{ID: newID(), GUID: guid, LibraryID: series.LibraryID, Type: model.TypeSeason, ParentID: &series.ID, Title: title, IndexAired: &num}
	if err := s.store.UpsertItem(ctx, season); err != nil {
		slog.Warn("metadata: creating a season from .nfo files", "series", series.Title, "season", num, "err", err)
		return nil
	}
	return season
}

// placeInSeason moves an episode under season when it sits elsewhere (season nil: it
// stays where it is) and gives it the episode number index (and end, the last episode a
// multi-episode file holds) when set.
func (s *Service) placeInSeason(ctx context.Context, episodeID string, season *model.Item, index, end *int) error {
	ep, err := s.store.GetItem(ctx, episodeID)
	if err != nil || ep == nil {
		return err
	}
	inSeason := season == nil || (ep.ParentID != nil && *ep.ParentID == season.ID && ep.SeasonAired != nil && season.IndexAired != nil && *ep.SeasonAired == *season.IndexAired)
	indexed := index == nil || (ep.IndexAired != nil && *ep.IndexAired == *index)
	ended := end == nil || (ep.IndexAiredEnd != nil && *ep.IndexAiredEnd == *end)
	if inSeason && indexed && ended {
		return nil
	}
	if season != nil {
		ep.ParentID = &season.ID
		ep.SeasonAired = season.IndexAired
	}
	if index != nil {
		ep.IndexAired = index
	}
	if end != nil {
		ep.IndexAiredEnd = end
	}
	return s.store.UpsertItem(ctx, ep)
}

// localSeasonPosters picks up season posters: "seasonNN-poster" (or
// "season-specials-poster") in the show folder, a poster inside the season's own folder
// ("Season NN", "Season N", "Specials"), and "season-all-poster" for any season without
// one of its own.
func (s *Service) localSeasonPosters(ctx context.Context, series *model.Item, dir string) {
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		return
	}
	all := firstFile(dir, withExts("season-all-poster")...)
	for _, se := range seasons {
		if se.IndexAired == nil {
			continue
		}
		n := *se.IndexAired
		names := []string{fmt.Sprintf("season%02d-poster", n), fmt.Sprintf("season%d-poster", n)}
		folders := []string{fmt.Sprintf("Season %02d", n), fmt.Sprintf("Season %d", n)}
		if n == 0 {
			names = append([]string{"season-specials-poster"}, names...)
			folders = append(folders, "Specials")
		}
		src := firstFile(dir, withExts(names...)...)
		for _, f := range folders {
			if src == "" {
				src = firstFile(filepath.Join(dir, f), withExts("poster", "folder", "cover")...)
			}
		}
		if src == "" {
			src = all
		}
		if src != "" {
			s.fetchImage(ctx, se.ID, "poster", src)
		}
	}
}
