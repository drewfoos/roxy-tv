package discover

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Films of a series: an anime's films are listed on its Discover page, between the
// seasons they sit between. Listing is all most of them get: a film is requested when
// somebody asks for it. The one exception is a film that AniList's own links place
// between two seasons of this show (Bunny Girl Senpai's three films between its two
// seasons): the main Request button names those and brings them along, and a request
// made that way keeps bringing such films as they are announced.
//
// The rules, the same for every client because only the server applies them:
//
//   - Who may be acquired without being asked for one by one (eligible): a film that
//     AniList links as the sequel of season A, the prequel of season B, or on the run of
//     films joining A to B, where A and B are neighbouring seasons of THIS show, every
//     such link names the same gap, and the film came out between the two. A release
//     date alone never does it. A side story, recap, alternate version or spin-off, a
//     film before the first or after the last season, one whose links disagree, and one
//     TMDB cannot be matched to for certain are all listed and left to be asked for.
//   - Authorization: films come along only with a request that said so (the button
//     named them). That request remembers it (store.Request.FilmAuto) together with the
//     seasons it covered, by AniList id (FilmSeasons). Later films are acquired only
//     for such a request and only between two seasons on that list: a season announced,
//     split or renumbered afterwards has an id that is not on it, so it widens nothing
//     until somebody requests again. Owning the seasons authorizes nothing.
//   - No duplicates: a film in the library (by AniList or TMDB id), already requested,
//     requested once before (the "storyfilm:" marker) or already in Radarr is not
//     requested.

// storyGraphTTL: how long a series' AniList links are trusted before they are read again.
const storyGraphTTL = 24 * time.Hour

// seasonPad: how far an AniList season's start may be from the season TMDB lists
// (pre-airings, time zones) and still be that season.
const seasonPad = 45 * 24 * time.Hour

// maxStoryFilms bounds one series' list (Detective Conan has a film a year).
const maxStoryFilms = 40

// Film is one film on a series' Discover page.
type Film struct {
	AniListID int    `json:"anilistId"`
	TMDBID    int    `json:"tmdbId,omitempty"` // 0: TMDB's film could not be told for certain; it cannot be requested
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Date      string `json:"date,omitempty"`
	Poster    string `json:"poster,omitempty"`
	// AfterSeason: the season number (as this page lists seasons) the film is shown
	// after; 0 before the first; -1 apart from the seasons ("More films").
	AfterSeason int `json:"afterSeason"`
	// Eligible: may come along with a request for the series.
	Eligible bool `json:"eligible"`
	// Note says why a film is only listed ("Side story", "After the last season").
	Note          string `json:"note,omitempty"`
	LibraryItemID string `json:"libraryItemId,omitempty"`
	RequestStatus string `json:"requestStatus,omitempty"`
	InRadarr      bool   `json:"inRadarr,omitempty"`
	// WithRequest: the page's main Request button brings this film.
	WithRequest bool `json:"withRequest"`
	between     [2]int
}

// Films is the answer of GET /v1/discover/tv/{id}/films.
type Films struct {
	Films []Film `json:"films"`
	// Auto: the series' request brings eligible films as they are announced.
	Auto bool `json:"auto"`
	// RequestsEnabled: films can be requested at all (Radarr is set up for requests).
	RequestsEnabled bool `json:"requestsEnabled"`
}

// filmMatch is TMDB's film for an AniList one (ID 0: none for certain; Why says why).
type filmMatch struct {
	ID     int    `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Poster string `json:"poster,omitempty"`
	Date   string `json:"date,omitempty"`
	Why    string `json:"why,omitempty"`
}

// storyCache is what is kept per series (setting "storygraph:<tmdb id>").
type storyCache struct {
	At    time.Time            `json:"at"`
	Graph *metadata.StoryGraph `json:"graph,omitempty"` // nil: AniList does not know the series
	TMDB  map[int]filmMatch    `json:"tmdb,omitempty"`  // AniList film id -> TMDB
}

// placedFilm is where placeFilms put one film.
type placedFilm struct {
	Film     metadata.StoryFilm
	After    int    // season number shown after; 0 before the first; -1 apart
	Between  [2]int // the AniList ids of the two seasons, when eligible
	Eligible bool
	Note     string
}

var reRecapFilm = regexp.MustCompile(`(?i)\b(recaps?|digest|compilation|summary|sou?shuu?hen|omnibus)\b|総集編`)

// fullDate parses 2019-06-15 (and 2019-06 as the 1st); anything less is no date.
func fullDate(s string) (time.Time, bool) {
	if len(s) == 7 {
		s += "-01"
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// showSeasons says which of the graph's seasons belong to the show as this page lists
// it, and under which listed season number. AniList's chain runs on into sequel series
// (Dragon Ball into Dragon Ball Z) that TMDB keeps as shows of their own: a season
// belongs when it starts inside the show's run, from the listed season it falls in.
// Nothing belongs (ok false) when no season starts where the show's first one does:
// then this is likely another series of the same name.
func showSeasons(g *metadata.StoryGraph, seasons []Season, lastAir string) (number map[int]int, ok bool) {
	type listed struct {
		n  int
		at time.Time
	}
	var ls []listed
	for _, sn := range seasons {
		if sn.Special {
			continue
		}
		if at, ok := fullDate(sn.AirDate); ok {
			ls = append(ls, listed{sn.Number, at})
		}
	}
	if g == nil || len(ls) == 0 {
		return nil, false
	}
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].at.Before(ls[j].at) })
	end := ls[len(ls)-1].at.Add(seasonPad)
	if t, ok := fullDate(lastAir); ok && t.After(end) {
		end = t
	}
	number = map[int]int{}
	anchored := false
	for _, s := range g.Seasons {
		at, ok := fullDate(s.Start)
		if !ok || at.After(end) {
			continue
		}
		n := -1
		for _, l := range ls {
			if !l.at.Add(-seasonPad).After(at) {
				n = l.n
			}
		}
		if n < 0 {
			continue
		}
		number[s.ID] = n
		if d := at.Sub(ls[0].at); d > -seasonPad && d < seasonPad {
			anchored = true
		}
	}
	if !anchored {
		return nil, false
	}
	return number, true
}

// placeFilms applies the eligibility rule above to a series' AniList graph. Pure: the
// same graph, seasons and dates always give the same answer.
func placeFilms(g *metadata.StoryGraph, seasons []Season, lastAir string) []placedFilm {
	number, ok := showSeasons(g, seasons, lastAir)
	if !ok {
		return nil
	}
	// Neighbours in AniList's chain, counted only while both are seasons of this show.
	idx := map[int]int{}
	start := map[int]time.Time{}
	for i, s := range g.Seasons {
		idx[s.ID] = i
		if t, ok := fullDate(s.Start); ok {
			start[s.ID] = t
		}
	}
	in := func(id int) bool { _, ok := number[id]; return ok }
	next := func(id int) int {
		if i := idx[id] + 1; i < len(g.Seasons) && in(g.Seasons[i].ID) {
			return g.Seasons[i].ID
		}
		return 0
	}
	prev := func(id int) int {
		if i := idx[id] - 1; i >= 0 && in(g.Seasons[i].ID) {
			return g.Seasons[i].ID
		}
		return 0
	}
	lastNumber := 0
	for _, sn := range seasons {
		if !sn.Special && sn.Number > lastNumber {
			lastNumber = sn.Number
		}
	}
	var out []placedFilm
	for _, f := range g.Films {
		gaps := map[[2]int]bool{}
		other := ""
		touches := false
		for _, l := range f.Links {
			if !in(l.Season) {
				continue
			}
			touches = true
			switch l.Relation {
			case "SEQUEL":
				gaps[[2]int{l.Season, next(l.Season)}] = true
			case "PREQUEL":
				gaps[[2]int{prev(l.Season), l.Season}] = true
			case "SUMMARY", "COMPILATION":
				other = "Recap"
			case "ALTERNATIVE":
				if other == "" {
					other = "Alternate version"
				}
			case "SPIN_OFF":
				if other == "" {
					other = "Spin-off"
				}
			default:
				if other == "" {
					other = "Side story"
				}
			}
		}
		for _, p := range f.Paths {
			a, b := p.After, p.Before
			if !in(a) && !in(b) {
				continue
			}
			touches = true
			switch {
			case in(a) && in(b) && next(a) == b:
				gaps[[2]int{a, b}] = true
			case in(a) && in(b):
				// Joined by films yet not neighbours: the links disagree with the order.
				gaps[[2]int{a, next(a)}] = true
				gaps[[2]int{prev(b), b}] = true
			case in(a):
				gaps[[2]int{a, next(a)}] = true // the run leaves the show: after a
			default:
				gaps[[2]int{prev(b), b}] = true
			}
		}
		if !touches {
			continue // a film of a sequel series, not of this show
		}
		p := placedFilm{Film: f, After: -1}
		var gap [2]int
		for k := range gaps {
			gap = k
		}
		at, dated := fullDate(f.Start)
		switch {
		case len(gaps) > 1:
			p.Note = "Can't tell where it fits"
			if other != "" {
				p.Note = other
			}
		case len(gaps) == 0:
			p.Note = other
		default:
			a, b := gap[0], gap[1]
			between := a != 0 && b != 0 && dated && !at.Before(start[a]) && !at.After(start[b])
			switch {
			case a != 0 && b != 0 && !between:
				p.Note = "Can't tell where it fits"
			case a == 0:
				p.After, p.Note = 0, "Before the first season"
				if number[b] > 1 {
					p.After, p.Note = -1, "Can't tell where it fits"
				}
			case b == 0:
				p.After, p.Note = number[a], "After the latest season"
				if number[a] != lastNumber {
					p.Note = "Not between two seasons for certain"
				}
			default:
				p.After = number[a]
			}
			switch {
			case other != "":
				p.Note = other
			case reRecapFilm.MatchString(f.Title) || reRecapFilm.MatchString(f.Romaji):
				p.Note = "Recap"
			case between:
				p.Eligible, p.Between = true, gap
			}
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.After < 0) != (b.After < 0) {
			return b.After < 0
		}
		if a.After != b.After {
			return a.After < b.After
		}
		if a.Film.Start != b.Film.Start {
			return a.Film.Start < b.Film.Start
		}
		return a.Film.ID < b.Film.ID
	})
	if len(out) > maxStoryFilms {
		out = out[:maxStoryFilms]
	}
	return out
}

func storyKey(tmdbID int) string { return fmt.Sprintf("storygraph:%d", tmdbID) }

// story is a series' AniList graph with its films matched to TMDB: the saved copy while
// it is fresh, else read again (and the saved copy when AniList cannot be reached).
// Nil without error: not an anime, or AniList does not know it.
func (s *Service) story(ctx context.Context, det *Detail) (*storyCache, error) {
	if det == nil || det.Type != "tv" || !det.IsAnime || s.storyGraph == nil {
		return nil, nil
	}
	var saved storyCache
	have, _ := s.store.GetSetting(ctx, storyKey(det.TMDBID), &saved)
	if have && time.Since(saved.At) < storyGraphTTL {
		return &saved, nil
	}
	// One series at a time: AniList is paced, and two pages opened together would
	// otherwise both walk the same chain.
	s.storyMu.Lock()
	defer s.storyMu.Unlock()
	var again storyCache
	if ok, _ := s.store.GetSetting(ctx, storyKey(det.TMDBID), &again); ok && time.Since(again.At) < storyGraphTTL {
		return &again, nil
	}
	anilistID := 0
	if det.LibraryItemID != "" {
		if v, err := s.store.ProviderID(ctx, det.LibraryItemID, "anilist"); err == nil {
			anilistID, _ = strconv.Atoi(v)
		}
	}
	g, err := s.storyGraph(ctx, anilistID, det.Title, det.Year)
	if err != nil {
		if have {
			slog.Warn("discover: AniList not reached, using the saved links", "title", det.Title, "err", err)
			return &saved, nil
		}
		return nil, err
	}
	fresh := storyCache{At: time.Now(), Graph: g, TMDB: map[int]filmMatch{}}
	for _, p := range placeFilms(g, det.Seasons, det.lastAir) {
		// A match made before stays: the same film is not looked up every day.
		if m, ok := saved.TMDB[p.Film.ID]; ok && m.ID != 0 {
			fresh.TMDB[p.Film.ID] = m
			continue
		}
		m, err := s.matchFilm(ctx, p.Film)
		if err != nil {
			m = filmMatch{Why: "TMDB could not be asked"}
		}
		fresh.TMDB[p.Film.ID] = m
	}
	if err := s.store.SetSetting(ctx, storyKey(det.TMDBID), fresh); err != nil {
		slog.Warn("discover: saving a series' film links", "title", det.Title, "err", err)
	}
	return &fresh, nil
}

// matchFilm is TMDB's film for an AniList one, by its English then its romaji title.
func (s *Service) matchFilm(ctx context.Context, f metadata.StoryFilm) (filmMatch, error) {
	why := "no title"
	for _, title := range []string{f.Title, f.Romaji} {
		if title == "" {
			continue
		}
		e, w, err := s.findFilm(ctx, title, f.Year)
		if err != nil {
			return filmMatch{}, err
		}
		if e != nil {
			return filmMatch{ID: e.ID, Title: e.Title, Poster: e.PosterPath, Date: e.ReleaseDate}, nil
		}
		why = w
		if f.Romaji == f.Title {
			break
		}
	}
	return filmMatch{Why: why}, nil
}

// Films lists a series' films for its Discover page.
func (s *Service) Films(ctx context.Context, tmdbID int) (*Films, error) {
	det, err := s.Detail(ctx, "tv", tmdbID)
	if err != nil {
		return nil, err
	}
	return s.filmsOf(ctx, det)
}

func (s *Service) filmsOf(ctx context.Context, det *Detail) (*Films, error) {
	out := &Films{Films: []Film{}, RequestsEnabled: s.RequestsEnabled("movie")}
	if det.Request != nil {
		out.Auto = det.Request.FilmAuto
	}
	sc, err := s.story(ctx, det)
	if err != nil || sc == nil || sc.Graph == nil {
		return out, err
	}
	placed := placeFilms(sc.Graph, det.Seasons, det.lastAir)
	items := make([]Item, 0, len(placed))
	for _, p := range placed {
		f := Film{AniListID: p.Film.ID, Title: p.Film.Title, Year: p.Film.Year, Date: p.Film.Start, Poster: p.Film.Poster,
			AfterSeason: p.After, Eligible: p.Eligible, Note: p.Note, between: p.Between}
		if m := sc.TMDB[p.Film.ID]; m.ID != 0 {
			f.TMDBID = m.ID
			if m.Title != "" {
				f.Title = m.Title
			}
			if m.Poster != "" {
				f.Poster = s.tmdb.ImageURL("w185", m.Poster)
			}
		} else {
			// Shown, never requested: nobody can say which film Radarr would fetch.
			f.Eligible = false
			if f.Note == "" {
				f.Note = "Not matched on TMDB for certain"
			}
		}
		out.Films = append(out.Films, f)
		items = append(items, Item{TMDBID: f.TMDBID, Type: "movie"})
	}
	s.annotate(ctx, items)
	var ids []string
	for _, f := range out.Films {
		ids = append(ids, strconv.Itoa(f.AniListID))
	}
	byAniList, _ := s.store.ItemsByProviderIDs(ctx, "anilist", ids)
	// The main button asks for every season the library lacks and nobody requested;
	// with none left there is no such button, and films are asked for one by one.
	button := false
	for _, sn := range det.Seasons {
		if !sn.Special && !sn.Requested && (sn.Known == 0 || sn.Have < sn.Episodes) {
			button = true
		}
	}
	button = button && s.RequestsEnabled("tv") && s.RequestsEnabled("movie")
	for i := range out.Films {
		f := &out.Films[i]
		if f.TMDBID != 0 {
			f.LibraryItemID, f.RequestStatus = items[i].LibraryItemID, items[i].RequestStatus
		}
		if li, ok := byAniList["anilist:"+strconv.Itoa(f.AniListID)]; ok && f.LibraryItemID == "" && li.Type == model.TypeMovie {
			f.LibraryItemID = li.ID
		}
		if f.TMDBID == 0 || f.LibraryItemID != "" || f.RequestStatus != "" {
			continue
		}
		f.InRadarr = s.inRadarr(ctx, f.TMDBID)
		if !f.Eligible || f.InRadarr || !button {
			continue
		}
		var by string
		if asked, _ := s.store.GetSetting(ctx, storyMarker(f.TMDBID), &by); asked {
			continue // requested once already; a request someone removed is not made again
		}
		f.WithRequest = true
	}
	return out, nil
}

// inRadarr: Radarr already has the film (remembered for two minutes; a page asks per film).
func (s *Service) inRadarr(ctx context.Context, tmdbID int) bool {
	_, radarr := s.clients()
	if radarr == nil {
		return false
	}
	key := strconv.Itoa(tmdbID)
	if v, ok := s.radarrHas.get(key); ok {
		return v
	}
	has, err := radarr.HasMovie(ctx, tmdbID)
	if err != nil {
		return false
	}
	s.radarrHas.put(key, has)
	return has
}

func storyMarker(tmdbID int) string { return fmt.Sprintf("storyfilm:%d", tmdbID) }

// coveredSeasons are the AniList ids of the show's seasons a request covers: those it
// asked for and those the library already holds.
func coveredSeasons(det *Detail, g *metadata.StoryGraph) []int {
	number, ok := showSeasons(g, det.Seasons, det.lastAir)
	if !ok {
		return nil
	}
	covered := map[int]bool{}
	for _, sn := range det.Seasons {
		if !sn.Special && (sn.Requested || sn.Known > 0) {
			covered[sn.Number] = true
		}
	}
	var out []int
	for _, s := range g.Seasons {
		if n, ok := number[s.ID]; ok && covered[n] {
			out = append(out, s.ID)
		}
	}
	return out
}

// RequestWithFilms is the main Request button on a page that named films: the series
// request, then the films. [films] are TMDB ids, the ones the page showed on the button;
// only those this server itself would put on the button count (Film.WithRequest, read
// before the request is made), so a client cannot widen the rule. With none of them
// left this is a plain series request and nothing is remembered.
//
// Otherwise the request remembers that films were asked for and which seasons it
// covered, so films announced later between those come too. A film that fails is logged
// and left for its own Request button; the series request stands.
func (s *Service) RequestWithFilms(ctx context.Context, tmdbID int, user *model.Principal, seasons []int, films []int) (*store.Request, error) {
	var chosen []Film
	if len(films) > 0 {
		if det, err := s.Detail(ctx, "tv", tmdbID); err == nil {
			if list, err := s.filmsOf(ctx, det); err == nil {
				for _, f := range list.Films {
					if f.WithRequest && contains(films, f.TMDBID) {
						chosen = append(chosen, f)
					}
				}
			}
		}
	}
	r, err := s.Request(ctx, "tv", tmdbID, user, seasons, nil)
	if err != nil || len(chosen) == 0 {
		return r, err
	}
	det, err := s.Detail(ctx, "tv", tmdbID)
	if err != nil || det.Request == nil {
		return r, nil
	}
	sc, err := s.story(ctx, det)
	if err != nil || sc == nil || sc.Graph == nil {
		return r, nil
	}
	r = det.Request
	for _, id := range coveredSeasons(det, sc.Graph) {
		if !contains(r.FilmSeasons, id) {
			r.FilmSeasons = append(r.FilmSeasons, id)
		}
	}
	r.FilmAuto = true
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		return nil, err
	}
	for _, f := range chosen {
		if contains(r.FilmSeasons, f.between[0]) && contains(r.FilmSeasons, f.between[1]) {
			s.requestFilm(ctx, r, f.TMDBID, f.AniListID, user)
		}
	}
	return r, nil
}

// AcquireStoryFilms brings the films announced since a request was made: for every sent
// series request that asked for films (FilmAuto), each eligible film between two of the
// seasons that request covered. Run after RetryPending, every 10 minutes; AniList is
// read at most once a day per series.
func (s *Service) AcquireStoryFilms(ctx context.Context) {
	if !s.RequestsEnabled("movie") {
		return
	}
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return
	}
	for i := range all {
		r := all[i]
		if r.MediaType != "tv" || r.Status != "sent" || !r.FilmAuto || len(r.FilmSeasons) < 2 {
			continue
		}
		det, err := s.Detail(ctx, "tv", r.TMDBID)
		if err != nil {
			continue
		}
		sc, err := s.story(ctx, det)
		if err != nil || sc == nil || sc.Graph == nil {
			continue
		}
		for _, p := range placeFilms(sc.Graph, det.Seasons, det.lastAir) {
			m := sc.TMDB[p.Film.ID]
			if !p.Eligible || m.ID == 0 || !contains(r.FilmSeasons, p.Between[0]) || !contains(r.FilmSeasons, p.Between[1]) {
				continue
			}
			s.requestFilm(ctx, &r, m.ID, p.Film.ID, nil)
		}
	}
}

// requestFilm asks Radarr for one film of a series request, unless something has it
// already. [user] is who pressed the button (a basic user's film waits for approval
// like any request of theirs); nil is the background run, for a request already sent.
func (s *Service) requestFilm(ctx context.Context, series *store.Request, tmdbID, anilistID int, user *model.Principal) bool {
	if have, err := s.store.ItemsByProviderIDs(ctx, "anilist", []string{strconv.Itoa(anilistID)}); err != nil || len(have) > 0 {
		return false
	}
	// The film is claimed before anything else happens: one statement that only one caller
	// can win (store.ClaimSetting), so two runs at once cannot both pass a "not requested
	// yet" check and both send it. The claim stays (a film request someone deletes is not
	// made again); it is given back only when this attempt stops before saving anything.
	marker := storyMarker(tmdbID)
	won, err := s.store.ClaimSetting(ctx, marker, series.ID)
	if err != nil || !won {
		return false
	}
	release := func() { _ = s.store.DeleteSetting(ctx, marker) }
	if existing, err := s.store.GetRequest(ctx, "movie", tmdbID); err != nil {
		release()
		return false
	} else if existing != nil {
		return false
	}
	det, err := s.Detail(ctx, "movie", tmdbID)
	if err != nil {
		release()
		return false
	}
	if det.LibraryItemID != "" {
		return false
	}
	// Radarr has it already (added by hand, or by another app): it is Radarr's, and a
	// request here would re-monitor and search a film somebody may have set aside.
	_, radarr := s.clients()
	if radarr == nil {
		release()
		return false
	}
	if has, err := radarr.HasMovie(ctx, tmdbID); err != nil || has {
		release()
		return false
	}
	// The request is saved before Radarr hears of it, as "pending". If the send works and
	// the save after it fails, Roxy still knows the request: the pending row is sent again
	// by RetryPending, and adding a film Radarr already has returns that film (arr.AddMovie)
	// rather than a second one. A send nobody recorded cannot happen.
	r := &store.Request{ID: newID(), MediaType: "movie", TMDBID: tmdbID, Title: det.Title, Year: det.Year, PosterPath: det.Poster,
		UserID: series.UserID, UserName: series.UserName, Status: "pending"}
	if user != nil {
		r.UserID, r.UserName = user.UserID, s.requesterName(ctx, user)
	}
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		slog.Warn("discover: saving story film request", "film", det.Title, "err", err)
		release()
		return false
	}
	if user != nil {
		s.sendOrHold(ctx, r, det, user)
	} else {
		s.send(ctx, r, det)
	}
	if err := s.store.UpsertRequest(ctx, r); err != nil {
		slog.Warn("discover: saving story film request after sending (retried as pending)", "film", det.Title, "err", err)
	}
	s.radarrHas.put(strconv.Itoa(tmdbID), true)
	slog.Info("discover: film requested with its series", "series", series.Title, "film", det.Title, "tmdb", tmdbID, "status", r.Status, "auto", user == nil)
	return true
}

// requesterName names who asked: the profile when several people share one login.
func (s *Service) requesterName(ctx context.Context, user *model.Principal) string {
	name := ""
	if user.User != nil {
		name = user.User.Name
	}
	if user.ScopeID != "" {
		if pr, err := s.store.GetProfile(ctx, user.ScopeID); err == nil && pr != nil && pr.Name != "" {
			name = pr.Name
		}
	}
	return name
}
