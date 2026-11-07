package metadata

import (
	"bytes"
	"context"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"image"
	"image/jpeg"
	"image/png" // AniList covers are sometimes PNG (re-encoded to JPEG); logos are kept as PNG

	_ "golang.org/x/image/webp" // artwork next to the media may be .webp (re-encoded to JPEG/PNG)
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/imaging"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/netguard"
	"github.com/roxy-media/roxy/server/internal/stills"
	"github.com/roxy-media/roxy/server/internal/store"
)

const (
	// refreshInterval is how long a successful lookup stays fresh.
	refreshInterval = 7 * 24 * time.Hour
	// retryFailed is how long an unmatched item is left alone (in-process) before the
	// next scan may try it again.
	retryFailed = time.Hour
	// retryUnmatched is how long a title no provider matched is left alone across
	// restarts (the last attempt is stored on the item).
	retryUnmatched = 24 * time.Hour
	// maxRetries is how many times in a row a lookup a provider failed to answer is
	// re-queued (after retryDelay, doubling) before it waits for the next scan.
	maxRetries = 4
	queueSize  = 1024
)

// Service runs metadata lookups off a work queue: one worker goroutine, items by id.
type Service struct {
	store     *store.Store
	cfg       *config.Config
	providers []Provider // in default preference order (TMDB first when configured)
	anilist   Provider
	tmdb      *TMDB
	images    *http.Client
	ff        *ffmpeg.FFmpeg // frame grabs for episodes with no still; nil without ffmpeg

	queue chan job
	force bool // the job being processed was a forced refresh (worker-local)
	// replaceImages: the job re-downloads art even when the provider's URL is unchanged
	// (worker-local; locked images are still kept).
	replaceImages bool
	mu            sync.Mutex
	pending       map[string]bool      // queued but not yet picked up (dedupe)
	failed        map[string]time.Time // top-level item id -> last unmatched attempt
	retries       map[string]int       // top-level item id -> provider failures in a row
	retryDelay    time.Duration        // first re-queue delay after a provider failure

	// OnSeasonMal is called with a season's id when it gets a new MyAnimeList id, so skip
	// segments can be looked up for it (main wires it to the segment detector).
	OnSeasonMal func(seasonID string)
}

type job struct {
	itemID string
	force  bool
	images bool // also re-download images whose URL did not change
}

// TMDB exposes the TMDB client when it has a key (Discover), else nil.
func (s *Service) TMDB() *TMDB {
	if s.tmdb.HasKey() {
		return s.tmdb
	}
	return nil
}

// TMDBProvider is the TMDB client whether or not it has a key yet (so Discover can be
// set up once and come alive when a key is entered).
func (s *Service) TMDBProvider() *TMDB { return s.tmdb }

func (s *Service) tmdbFromProviders() *TMDB {
	for _, p := range s.providers {
		if t, ok := p.(*TMDB); ok {
			return t
		}
	}
	return nil
}

// AniList exposes the AniList client for non-matching uses (profile avatars).
func (s *Service) AniList() *AniList { a, _ := s.anilist.(*AniList); return a }

func New(st *store.Store, cfg *config.Config) *Service {
	s := &Service{
		store:      st,
		cfg:        cfg,
		images:     &http.Client{Timeout: 30 * time.Second, CheckRedirect: netguard.CheckRedirect},
		queue:      make(chan job, queueSize),
		pending:    map[string]bool{},
		failed:     map[string]time.Time{},
		retries:    map[string]int{},
		retryDelay: 2 * time.Minute,
	}
	s.anilist = NewAniList()
	// Always there; without a key it matches nothing until one is entered (setup / settings).
	s.tmdb = NewTMDB(cfg.TMDBAPIKey)
	if !s.tmdb.HasKey() {
		slog.Warn("metadata: no TMDB key yet; AniList only until one is entered in setup or settings")
	}
	s.providers = append(s.providers, s.tmdb, s.anilist)
	return s
}

// Start launches the worker; it stops when ctx ends.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-s.queue:
				s.mu.Lock()
				delete(s.pending, j.itemID)
				s.mu.Unlock()
				s.processSafe(ctx, j)
			}
		}
	}()
}

// Enqueue schedules a lookup for an item (or the series/movie above it). Cheap and
// non-blocking; duplicates already waiting in the queue are dropped.
func (s *Service) Enqueue(itemID string) { s.enqueue(itemID, false) }

// Refresh schedules a forced lookup, ignoring the 7-day freshness window.
func (s *Service) Refresh(itemID string) { s.enqueue(itemID, true) }

// RefreshImages is Refresh that also re-downloads every image (Library > Refresh >
// Replace images): a broken or low-quality cached copy is fetched again.
func (s *Service) RefreshImages(itemID string) {
	s.enqueueJob(job{itemID: itemID, force: true, images: true})
}

func (s *Service) enqueue(itemID string, force bool) { s.enqueueJob(job{itemID: itemID, force: force}) }

func (s *Service) enqueueJob(j job) {
	itemID, force := j.itemID, j.force
	if itemID == "" {
		return
	}
	s.mu.Lock()
	if s.pending[itemID] && !force {
		s.mu.Unlock()
		return
	}
	s.pending[itemID] = true
	s.mu.Unlock()
	select {
	case s.queue <- j:
	default:
		s.mu.Lock()
		delete(s.pending, itemID)
		s.mu.Unlock()
		slog.Warn("metadata: queue full, dropping", "item", itemID)
	}
}

func (s *Service) processSafe(ctx context.Context, j job) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("metadata: panic processing item", "item", j.itemID, "panic", r)
		}
	}()
	if err := s.process(ctx, j); err != nil {
		slog.Warn("metadata: lookup failed", "item", j.itemID, "err", err)
	}
}

func (s *Service) process(ctx context.Context, j job) error {
	topID, err := s.store.TopLevelID(ctx, j.itemID)
	if err != nil {
		return err
	}
	it, err := s.store.GetItem(ctx, topID)
	if err != nil || it == nil {
		return err
	}
	// Whatever the lookup finds, every episode (and a film nobody has art for) ends up
	// with a picture: a frame of its own file where no still came from a provider or
	// from a file next to the media.
	defer s.fillStills(ctx, it)
	// Once it is matched to a provider (here or by its .nfo), a film or episode that left
	// the library under another name can be recognised by that identity and its watch
	// history brought over (store.SettleByProvider). Deferred: it needs this job's ids.
	defer func() {
		if n, err := s.store.SettleByProvider(ctx, it.ID); err != nil {
			slog.Warn("moved: matching departures by provider id", "item", it.Title, "err", err)
		} else if n > 0 {
			slog.Info("moved: watch history followed by provider id", "item", it.Title, "count", n)
		}
	}()
	s.force = j.force
	s.replaceImages = j.images
	// Details set by hand (or a custom show no provider knows) stay as they are.
	if locked, err := s.store.MetadataLocked(ctx, it.ID); err == nil && locked {
		return nil
	}
	// Kodi .nfo files and artwork next to the media (One Pace ships them) come first:
	// an .nfo with provider ids steers the lookup below; one without ids is the metadata.
	if handled, err := s.applyLocal(ctx, it); handled || err != nil {
		return err
	}
	// After the lookup below, however it ends: what the .nfo sets, episode .nfo files and
	// artwork next to the media win over the provider's (Jellyfin's order).
	defer s.overlayLocal(ctx, it)
	startedAt := time.Now()
	attempted, _ := s.store.MetadataAttemptedAt(ctx, it.ID)
	onlyNewEpisodes := false
	if !j.force {
		// .nfo files or artwork added or changed next to the media since the last lookup
		// count as a reason to look again, so "Scan for new files" picks them up.
		if it.MetadataRefreshedAt != nil && time.Since(*it.MetadataRefreshedAt) < refreshInterval && !s.localChangedSince(ctx, it, *it.MetadataRefreshedAt) && !s.bannerBackdrop(ctx, it) {
			if it.Type != model.TypeSeries {
				return nil
			}
			// Only episodes that arrived since the last attempt: the ones a provider could
			// not fill then (no data yet, unparseable names) do not trigger a lookup on
			// every start.
			n, err := s.store.CountUnrefreshedEpisodes(ctx, it.ID, attempted)
			if err != nil || n == 0 {
				return err
			}
			onlyNewEpisodes = true
		}
		s.mu.Lock()
		last, tried := s.failed[it.ID]
		s.mu.Unlock()
		if tried && time.Since(last) < retryFailed {
			return nil
		}
		// A title no provider matched is not asked again on every restart either.
		if it.MetadataRefreshedAt == nil && attempted != nil && time.Since(*attempted) < retryUnmatched {
			return nil
		}
	}

	hints := Hints{}
	if libs, err := s.store.ListLibraries(ctx); err == nil {
		for _, l := range libs {
			if l.ID == it.LibraryID {
				hints.Anime = l.AnimeMode
			}
		}
	}

	primary, secondary, failed, err := s.match(ctx, it, hints)
	if err != nil {
		// A provider that should have answered did not (AniList's 429, an outage): keep
		// everything as it is rather than overwrite it with the other provider's view,
		// and try again later.
		s.mu.Lock()
		s.failed[it.ID] = time.Now()
		s.mu.Unlock()
		s.retryLater(j)
		return err
	}
	s.mu.Lock()
	delete(s.retries, it.ID)
	s.mu.Unlock()
	if primary == nil {
		s.mu.Lock()
		s.failed[it.ID] = time.Now()
		s.mu.Unlock()
		if len(failed) == 0 {
			_ = s.store.SetMetadataAttempted(ctx, it.ID, startedAt)
		}
		slog.Info("metadata: no match", "item", it.Title, "type", it.Type, "anime", hints.Anime, "failed", failed)
		return nil
	}
	merged := merge(primary, secondary)
	if !onlyNewEpisodes {
		meta := itemMetadata(merged, it.Type)
		if it.Type == model.TypeSeries {
			// Season numbering follows TMDB (TVDB-style seasons), never AniList's chain of
			// sequels, which counts spin-offs and split cours as seasons of their own. A
			// season TMDB lists before it has any episodes (announced only) does not count.
			for _, r := range []*Result{primary, secondary} {
				if r != nil && r.Provider == "tmdb" {
					meta.SeasonCount = 0
					for _, se := range r.Seasons {
						if se.Number > 0 && se.EpisodeCount > 0 {
							meta.SeasonCount++
						}
					}
				}
			}
		}
		if err := s.store.UpdateItemMetadata(ctx, it.ID, meta); err != nil {
			return err
		}
		for _, typ := range model.ImageTypes {
			if u := merged.Images[typ]; u != "" {
				s.fetchImage(ctx, it.ID, typ, u)
			}
		}
		slog.Info("metadata: matched", "item", it.Title, "type", it.Type, "provider", primary.Provider, "title", merged.Title, "year", merged.Year, "ids", merged.ProviderIDs)
		if failed["anilist"] {
			// Related titles come from AniList; without its answer an empty list would
			// only erase the ones saved before.
			slog.Info("metadata: relations kept (AniList did not answer)", "item", it.Title)
		} else {
			rels := make([]store.ItemRelation, 0, len(merged.Related))
			for _, r := range merged.Related {
				rels = append(rels, store.ItemRelation{Provider: r.Provider, ProviderID: r.ProviderID, Relation: r.Type, Title: r.Title, Format: r.Format, Year: r.Year, PosterURL: r.Poster, Episodes: r.Episodes})
			}
			if err := s.store.ReplaceRelations(ctx, it.ID, rels); err != nil {
				slog.Warn("metadata: saving relations", "item", it.Title, "err", err)
			} else if len(rels) > 0 {
				slog.Info("metadata: relations", "item", it.Title, "count", len(rels), "seasons", len(merged.Seasons))
			}
		}
	}
	if it.Type == model.TypeSeries {
		n := s.applySeries(ctx, it, primary, secondary, onlyNewEpisodes)
		if onlyNewEpisodes && n == 0 {
			// Nothing the providers know about yet; back off instead of retrying every scan.
			s.mu.Lock()
			s.failed[it.ID] = time.Now()
			s.mu.Unlock()
		}
	}
	if len(failed) == 0 {
		_ = s.store.SetMetadataAttempted(ctx, it.ID, startedAt)
	}
	return nil
}

// retryLater re-queues a job whose lookup a provider failed to answer, after a delay
// that doubles with every failure in a row; after maxRetries it waits for the next scan
// or refresh (s.failed holds plain scans back for retryFailed meanwhile).
func (s *Service) retryLater(j job) {
	s.mu.Lock()
	n := s.retries[j.itemID]
	if n >= maxRetries {
		delete(s.retries, j.itemID)
		s.mu.Unlock()
		slog.Warn("metadata: giving up for now; the next scan or refresh tries again", "item", j.itemID)
		return
	}
	s.retries[j.itemID] = n + 1
	delay := s.retryDelay << n
	s.mu.Unlock()
	time.AfterFunc(delay, func() { s.enqueueJob(j) })
}

// errProviderDown is a provider error for an item that provider is responsible for.
type errProviderDown struct {
	provider string
	err      error
}

func (e *errProviderDown) Error() string { return e.provider + " did not answer: " + e.err.Error() }
func (e *errProviderDown) Unwrap() error { return e.err }

// mustAnswer reports whether the item's lookup is meaningless without p: AniList for
// anime (an anime-mode library, or a title already matched on AniList), TMDB for a title
// already matched on TMDB. Without them the other provider's answer would replace the
// metadata, art and related titles.
func mustAnswer(p Provider, it *model.Item, hints Hints) bool {
	switch p.Name() {
	case "anilist":
		return hints.Anime || it.ProviderIDs["anilist"] != ""
	case "tmdb":
		return it.ProviderIDs["tmdb"] != ""
	}
	return false
}

// match tries every provider: the first hit is primary (TMDB unless the library is in
// anime mode, where AniList leads), later hits enrich. AniList is always consulted so
// anime gets its anilist id even when TMDB matched first. A provider error is an error
// (errProviderDown) when that provider is responsible for the item (mustAnswer);
// otherwise the provider is skipped and named in failed.
func (s *Service) match(ctx context.Context, it *model.Item, hints Hints) (primary, secondary *Result, failed map[string]bool, err error) {
	order := make([]Provider, 0, len(s.providers))
	if hints.Anime {
		order = append(order, s.anilist)
		for _, p := range s.providers {
			if p != s.anilist {
				order = append(order, p)
			}
		}
	} else {
		order = append(order, s.providers...)
	}
	for _, p := range order {
		r, err := p.Match(ctx, it, hints)
		if err != nil {
			if mustAnswer(p, it, hints) {
				return nil, nil, nil, &errProviderDown{provider: p.Name(), err: err}
			}
			slog.Warn("metadata: provider error", "provider", p.Name(), "item", it.Title, "err", err)
			if failed == nil {
				failed = map[string]bool{}
			}
			failed[p.Name()] = true
			continue
		}
		if r == nil {
			slog.Debug("metadata: provider has no match", "provider", p.Name(), "item", it.Title)
			continue
		}
		if primary == nil {
			primary = r
		} else if secondary == nil {
			secondary = r
		}
	}
	return primary, secondary, failed, nil
}

// merge fills gaps in a with b and unions provider ids and images.
func merge(a, b *Result) *Result {
	if b == nil {
		return a
	}
	out := *a
	out.ProviderIDs = map[string]string{}
	out.Images = map[string]string{}
	for k, v := range b.ProviderIDs {
		out.ProviderIDs[k] = v
	}
	for k, v := range a.ProviderIDs {
		out.ProviderIDs[k] = v
	}
	for k, v := range b.Images {
		out.Images[k] = v
	}
	for k, v := range a.Images {
		out.Images[k] = v
	}
	// AniList's banner is a 1900x400 strip; TMDB's backdrop is real 16:9 key art, which
	// the TV's hero and title pages fill the screen with. Take TMDB's whichever side it
	// came from, and keep the banner only when there is nothing else.
	if v := tmdbBackdrop(a, b); v != "" {
		out.Images["backdrop"] = v
	}
	if out.Overview == "" {
		out.Overview = b.Overview
	}
	if out.Tagline == "" {
		out.Tagline = b.Tagline
	}
	if len(out.Genres) == 0 {
		out.Genres = b.Genres
	}
	// Advisories add up: TMDB's descriptors and AniList's tags see different things.
	out.Advisories = mergeAdvisories(a.Advisories, b.Advisories)
	if out.CommunityRating == 0 {
		out.CommunityRating = b.CommunityRating
	}
	if out.OfficialRating == "" {
		out.OfficialRating = b.OfficialRating
	}
	if out.Premiered == "" {
		out.Premiered = b.Premiered
	}
	if out.Status == "" {
		out.Status = b.Status
	}
	if out.Studio == "" {
		out.Studio = b.Studio
	}
	if out.Year == 0 {
		out.Year = b.Year
	}
	if len(out.Related) == 0 {
		out.Related = b.Related
	}
	if len(out.Seasons) <= 1 && len(b.Seasons) > len(out.Seasons) {
		// The secondary knows the season structure better (AniList chain vs a
		// single-season TMDB hit, or vice versa); take it for episode lookups.
		out.Seasons = b.Seasons
	}
	return &out
}

// mergeAdvisories unions two descriptor lists in a fixed, readable order.
// bannerBackdrop reports a title whose backdrop is still AniList's banner strip although
// TMDB knows it: a reason to look again, so the 16:9 art replaces the strip.
func (s *Service) bannerBackdrop(ctx context.Context, it *model.Item) bool {
	if it.ProviderIDs["tmdb"] == "" {
		return false
	}
	img, err := s.store.GetItemImage(ctx, it.ID, "backdrop")
	if err != nil || img == nil || img.Locked {
		return false
	}
	return strings.Contains(img.SourceURL, "anilistcdn/media/anime/banner")
}

// tmdbBackdrop returns the first TMDB backdrop among the results, or "".
func tmdbBackdrop(rs ...*Result) string {
	for _, r := range rs {
		if r != nil && r.Provider == "tmdb" && r.Images["backdrop"] != "" {
			return r.Images["backdrop"]
		}
	}
	return ""
}

func mergeAdvisories(a, b []string) []string {
	seen := map[string]bool{}
	for _, x := range append(append([]string{}, a...), b...) {
		seen[x] = true
	}
	var out []string
	for _, k := range AdvisoryOrder {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out
}

func itemMetadata(r *Result, t model.ItemType) store.ItemMetadata {
	m := store.ItemMetadata{
		Title:           r.Title,
		Overview:        r.Overview,
		Tagline:         r.Tagline,
		Genres:          r.Genres,
		Advisories:      r.Advisories,
		CommunityRating: r.CommunityRating,
		OfficialRating:  r.OfficialRating,
		Premiered:       r.Premiered,
		Studio:          r.Studio,
		ProviderIDs:     r.ProviderIDs,
	}
	if t == model.TypeSeries {
		m.Status = r.Status
	}
	if r.Year != 0 {
		y := r.Year
		m.Year = &y
	}
	return m
}

// applySeries fills season posters and episode titles/overviews/air dates/stills.
// Returns the number of episodes updated.
//
// Episode data comes from TMDB first whichever provider matched first: its seasons
// follow the TVDB-style numbering Sonarr files by, while AniList's Crunchyroll lists are
// per AniList entry and sometimes another season's. A file whose name carries an episode
// title (Sonarr's "Show - S00E03 - Title") is matched by that title within the season
// before its number: TVDB and TMDB number specials (and some shows' episodes)
// differently, so the number alone can name another episode.
func (s *Service) applySeries(ctx context.Context, series *model.Item, primary, secondary *Result, onlyNew bool) int {
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		slog.Warn("metadata: listing seasons", "series", series.Title, "err", err)
		return 0
	}
	fileTitles := s.episodeFiles(ctx, series)
	cache := map[string]*Season{} // "<provider>:<season>" -> fetched season (nil = unknown)
	fetch := func(r *Result, n int) *Season {
		if r == nil {
			return nil
		}
		key := fmt.Sprintf("%s:%d", r.Provider, n)
		if se, ok := cache[key]; ok {
			return se
		}
		var se *Season
		for _, p := range s.providers {
			if p.Name() != r.Provider {
				continue
			}
			var err error
			se, err = p.Episodes(ctx, r, n)
			if err != nil {
				slog.Warn("metadata: season lookup failed", "provider", p.Name(), "series", series.Title, "season", n, "err", err)
			}
		}
		cache[key] = se
		return se
	}
	// Episodes named by their air date (daily shows): filed under the provider's episode
	// that aired that day, before the numbered pass below.
	if n := s.matchDatedEpisodes(ctx, series, tmdbFirst(primary, secondary), fetch); n > 0 {
		slog.Info("metadata: episodes found by air date", "series", series.Title, "count", n)
		if seasons, err = s.store.Children(ctx, series.ID, model.TypeSeason); err != nil {
			return 0
		}
	}
	// How the library's seasons line up with each provider's (see layout.go).
	lengths := s.realSeasonLengths(ctx, seasons)
	layouts := map[*Result]*seasonLayout{}
	for _, r := range []*Result{primary, secondary} {
		if r != nil {
			layouts[r] = layoutFor(r, lengths)
			if layouts[r].split {
				slog.Debug("metadata: seasons split differently from the provider", "series", series.Title, "provider", r.Provider)
			}
		}
	}
	byPref := tmdbFirst(primary, secondary)
	src := placeholderSource(primary, secondary)
	// Provider episodes (in src's numbering, per library season) a file claimed by its
	// title although it carries another number: no "missing" placeholder for them.
	claimed := map[int]map[int]bool{}
	updated := 0
	for i := range seasons {
		season := &seasons[i]
		num := 1
		if season.IndexAired != nil {
			num = *season.IndexAired
		}
		if _, ok := season.Images["poster"]; !ok || !onlyNew {
			for _, r := range []*Result{primary, secondary} {
				if r == nil {
					continue
				}
				ps, ok := layouts[r].posterSeason(r, num, lengths[num])
				if !ok {
					continue
				}
				if se := fetch(r, ps); se != nil && se.Poster != "" {
					s.fetchImage(ctx, season.ID, "poster", se.Poster)
					break
				}
			}
		}
		// A forced refresh on a season whose provider data turned out to be wrong
		// (see SeasonInfo.NoEpisodeData) clears what an earlier run wrote so the
		// episodes fall back to "Episode N" until a provider with real data fills them.
		if s.force && num >= 1 && num-1 < len(primary.Seasons) && primary.Seasons[num-1].NoEpisodeData && fetch(secondary, num) == nil {
			if err := s.store.ResetEpisodeMetadata(ctx, season.ID); err == nil {
				slog.Info("metadata: season episode data untrusted, reset", "series", series.Title, "season", num, "provider", primary.Provider)
			}
			continue
		}
		episodes, err := s.store.Children(ctx, season.ID, model.TypeEpisode)
		if err != nil {
			continue
		}
		restarts := absoluteRestarts(episodes)
		for j := range episodes {
			ep := &episodes[j]
			if onlyNew && ep.MetadataRefreshedAt != nil {
				continue
			}
			sn, en := num, 0
			if ep.IndexAired != nil {
				en = *ep.IndexAired
			}
			// Absolute-numbered files ("Show - 013 - Title") carry index == absolute; map
			// them through the provider's season lengths. In a later season's folder the
			// numbers may instead restart at 1 (InuYasha's "The Final Act" folder holds
			// 01..26): then they are that season's own, unless the provider puts the
			// absolute number in that very season.
			abs := 0
			if ep.IndexAbsolute != nil && (ep.IndexAired == nil || *ep.IndexAired == *ep.IndexAbsolute) {
				abs = *ep.IndexAbsolute
				if num > 1 && restarts {
					inSeason := true
					for _, r := range byPref {
						if ms, _, ok := r.AbsoluteToAired(abs); ok {
							inSeason = ms != num
							break
						}
					}
					if inSeason {
						abs = 0
					}
				}
			}
			if en == 0 && abs == 0 {
				continue
			}
			toProvider := func(r *Result) (int, int, bool) {
				if abs > 0 {
					if ps, pe, ok := r.AbsoluteToAired(abs); ok {
						return ps, pe, true
					}
				}
				if en == 0 {
					return 0, 0, false
				}
				return layouts[r].toProvider(r, sn, en)
			}
			fileTitle := fileTitles[ep.ID]
			var hit *EpisodeResult
			if fileTitle != "" {
				for _, r := range byPref {
					ps, pe, ok := toProvider(r)
					if !ok {
						continue
					}
					if se := fetch(r, ps); se != nil {
						if k := matchEpisodeTitle(se.Episodes, fileTitle, pe); k >= 0 {
							hit = &se.Episodes[k]
							if r == src && hit.Episode != pe && (sn == 0 || !layouts[r].split) {
								if claimed[sn] == nil {
									claimed[sn] = map[int]bool{}
								}
								claimed[sn][hit.Episode] = true
							}
							break
						}
					}
				}
			}
			mismatch := false
			if hit == nil {
				for _, r := range byPref {
					ps, pe, ok := toProvider(r)
					if !ok {
						continue
					}
					if se := fetch(r, ps); se != nil {
						for k := range se.Episodes {
							if se.Episodes[k].Episode == pe {
								hit = &se.Episodes[k]
								break
							}
						}
					}
					if hit != nil {
						break
					}
				}
				// A special named after something else than the provider's special of the
				// same number (JUJUTSU KAISEN 0 filed as S00E01, TMDB's S00E01 a New Year's
				// short) is not that special: it keeps its own name.
				if hit != nil && sn == 0 && namesSomething(fileTitle) && !titleAgrees(fileTitle, hit.Title) {
					hit, mismatch = nil, true
				}
			}
			if hit == nil {
				if mismatch && ep.Title != fileTitle {
					if err := s.store.ResetEpisodeToFile(ctx, ep.ID, fileTitle); err == nil {
						slog.Info("metadata: special keeps its file's title", "series", series.Title, "episode", en, "title", fileTitle)
					}
				}
				slog.Debug("metadata: no episode data", "series", series.Title, "season", sn, "episode", en)
				continue
			}
			title := hit.Title
			if ep.IndexAired != nil && ep.IndexAiredEnd != nil && *ep.IndexAiredEnd > *ep.IndexAired {
				// A file holding several episodes (SpongeBob's segments as they aired):
				// every one's title, as Jellyfin shows them.
				title = joinedTitle(hit, *ep.IndexAiredEnd-*ep.IndexAired, byPref, toProvider, fetch)
			}
			meta := store.ItemMetadata{Title: title, Overview: hit.Overview, Premiered: hit.AirDate}
			if err := s.store.UpdateItemMetadata(ctx, ep.ID, meta); err != nil {
				slog.Warn("metadata: updating episode", "episode", ep.ID, "err", err)
				continue
			}
			if hit.Thumb != "" {
				s.fetchImage(ctx, ep.ID, "thumb", hit.Thumb)
			}
			updated++
		}
	}
	if updated > 0 {
		slog.Info("metadata: episodes updated", "series", series.Title, "count", updated)
	}
	if n, err := s.store.SyncChildLibrary(ctx, series.ID); err == nil && n > 0 {
		slog.Info("metadata: seasons/episodes moved to the series' library", "series", series.Title, "count", n)
	}
	s.createPlaceholders(ctx, series, seasons, src, otherResult(src, primary, secondary), fetch, layouts, lengths, claimed)
	s.storeSeasonIDs(ctx, series, primary, secondary, layouts, lengths)
	return updated
}

// matchDatedEpisodes files each real episode of the series that has an air date but no
// number (a file named "Show 2024.03.15") under the provider episode that aired that day:
// its season, number, title and synopsis. Returns how many were placed.
func (s *Service) matchDatedEpisodes(ctx context.Context, series *model.Item, results []*Result, fetch func(*Result, int) *Season) int {
	eps, _, err := s.store.ListItems(ctx, store.ItemQuery{ParentID: series.ID, Recursive: true, Types: []model.ItemType{model.TypeEpisode}})
	if err != nil {
		return 0
	}
	var dated []model.Item
	for _, e := range eps {
		if e.IndexAired == nil && e.IndexAbsolute == nil && !e.IsVirtual && len(e.Premiered) >= 10 {
			dated = append(dated, e)
		}
	}
	if len(dated) == 0 {
		return 0
	}
	byDate := map[string]EpisodeResult{}
	for _, r := range results {
		for _, si := range r.Seasons {
			if se := fetch(r, si.Number); se != nil {
				for _, ep := range se.Episodes {
					if _, seen := byDate[ep.AirDate]; !seen && ep.AirDate != "" {
						byDate[ep.AirDate] = ep
					}
				}
			}
		}
		if len(byDate) > 0 {
			break // the preferred provider's dates are enough
		}
	}
	placed := 0
	for _, e := range dated {
		hit, ok := byDate[e.Premiered[:10]]
		if !ok {
			continue
		}
		season := s.seasonByNumber(ctx, series, hit.Season, "")
		n := hit.Episode
		if err := s.placeInSeason(ctx, e.ID, season, &n, nil); err != nil {
			continue
		}
		_ = s.store.UpdateItemMetadata(ctx, e.ID, store.ItemMetadata{Title: hit.Title, Overview: hit.Overview, Premiered: hit.AirDate})
		placed++
	}
	return placed
}

// joinedTitle is the title of a file that holds span+1 episodes starting at hit: the
// provider's titles of all of them in order, joined with " / " ("Help Wanted / Reef Blower
// / Tea at the Treedome"). Taken from the provider season hit came from; hit's own title
// when that season does not list them all.
func joinedTitle(hit *EpisodeResult, span int, byPref []*Result, toProvider func(*Result) (int, int, bool), fetch func(*Result, int) *Season) string {
	for _, r := range byPref {
		ps, _, ok := toProvider(r)
		if !ok {
			continue
		}
		se := fetch(r, ps)
		if se == nil {
			continue
		}
		byNum := map[int]string{}
		for _, e := range se.Episodes {
			byNum[e.Episode] = e.Title
		}
		if byNum[hit.Episode] != hit.Title {
			continue // hit came from another provider's season
		}
		titles := make([]string, 0, span+1)
		for n := hit.Episode; n <= hit.Episode+span; n++ {
			t, ok := byNum[n]
			if !ok || t == "" {
				return hit.Title
			}
			titles = append(titles, t)
		}
		// One story in parts ("Psych: The Musical", or "Title (1)" / "Title (2)", "Title,
		// Part 2"): the shared title once, not repeated.
		if base := partlessTitle(titles[0]); base != "" {
			same := true
			for _, t := range titles[1:] {
				if partlessTitle(t) != base {
					same = false
					break
				}
			}
			if same {
				return strings.TrimSpace(reTitlePart.ReplaceAllString(titles[0], ""))
			}
		}
		return strings.Join(titles, " / ")
	}
	return hit.Title
}

// reTitlePart is a part number at the end of an episode title: " (1)", " (Part 2)",
// ", Part 2", " - Part II", " Pt. 3".
var reTitlePart = regexp.MustCompile(`(?i)[\s,:-]*(?:\((?:part|pt\.?)?\s*(?:\d+|[ivx]+)\)|(?:part|pt\.?)\s*(?:\d+|[ivx]+|one|two|three))\s*$`)

// partlessTitle is a title without its part number, lower case, for comparing parts.
func partlessTitle(t string) string {
	return strings.ToLower(strings.TrimSpace(reTitlePart.ReplaceAllString(t, "")))
}

// tmdbFirst lists the non-nil results with TMDB's first.
func tmdbFirst(rs ...*Result) []*Result {
	var out []*Result
	for _, r := range rs {
		if r != nil && r.Provider == "tmdb" {
			out = append(out, r)
		}
	}
	for _, r := range rs {
		if r != nil && r.Provider != "tmdb" {
			out = append(out, r)
		}
	}
	return out
}

// placeholderSource is the result whose seasons placeholders follow: TMDB's whenever
// TMDB matched (TVDB-style seasons), AniList's only without a TMDB match at all. AniList's
// chain of sequels counts spin-offs as seasons (Yu-Gi-Oh! GX and 5D's would become
// seasons 2 and 3 of Duel Monsters), and its Crunchyroll lists are sometimes another
// season's.
func placeholderSource(primary, secondary *Result) *Result {
	if rs := tmdbFirst(primary, secondary); len(rs) > 0 {
		return rs[0]
	}
	return nil
}

// otherResult is whichever of primary/secondary is not r.
func otherResult(r, primary, secondary *Result) *Result {
	if r == primary {
		return secondary
	}
	return primary
}

// absoluteRestarts reports whether a season's absolute-only files start at 1 (their
// numbers count within the season folder rather than across the show).
func absoluteRestarts(eps []model.Item) bool {
	lowest := 0
	for _, e := range eps {
		if e.IsVirtual || e.IndexAbsolute == nil || (e.IndexAired != nil && *e.IndexAired != *e.IndexAbsolute) {
			continue
		}
		if lowest == 0 || *e.IndexAbsolute < lowest {
			lowest = *e.IndexAbsolute
		}
	}
	return lowest == 1
}

// episodeFiles reads the media file names under a series: the episode title each one
// carries, by item id ("" when none), and the last episode of every multi-episode file
// (stored for files scanned before the store kept it).
func (s *Service) episodeFiles(ctx context.Context, series *model.Item) map[string]string {
	paths, err := s.store.PathsUnder(ctx, series.ID)
	if err != nil {
		return nil
	}
	titles := map[string]string{}
	for _, p := range paths {
		name := filepath.Base(p.Path)
		if t := library.FileEpisodeTitle(name); t != "" && titles[p.ItemID] == "" {
			titles[p.ItemID] = t
		}
		if pn := library.ParseEpisodeFile(name); pn.Episode != nil && pn.EpisodeEnd != nil && *pn.EpisodeEnd > *pn.Episode {
			_ = s.store.SetIndexEnd(ctx, p.ItemID, pn.EpisodeEnd)
		}
	}
	return titles
}

// compactTitle folds a title for comparing a file name's copy with a provider's: case,
// accents, punctuation and spaces all go ("God's Blessings on This Wonderful Choker!"
// and Sonarr's "Gods Blessings On This Wonderful Choker!" are the same).
func compactTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := accentFold[r]; ok {
			r = f
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameTitle: equal once compacted, or one a prefix of the other when the shorter still
// says enough (Sonarr shortens long titles).
func sameTitle(a, b string) bool {
	ca, cb := compactTitle(a), compactTitle(b)
	if ca == "" || cb == "" {
		return false
	}
	if ca == cb {
		return true
	}
	if len(ca) > len(cb) {
		ca, cb = cb, ca
	}
	return len(ca) >= 15 && strings.HasPrefix(cb, ca)
}

// titleCandidates is a file's title and, for a multi-episode file ("Help Wanted + Reef
// Blower + Tea at the Treedome"), its first part.
func titleCandidates(t string) []string {
	out := []string{t}
	for _, sep := range []string{" + ", " / "} {
		if i := strings.Index(t, sep); i > 0 {
			out = append(out, strings.TrimSpace(t[:i]))
		}
	}
	return out
}

// matchEpisodeTitle finds the provider episode a file's title names: an exact match
// before a shortened one, the episode numbered nearest the file's number among equals.
// -1 when none matches.
func matchEpisodeTitle(eps []EpisodeResult, fileTitle string, near int) int {
	best, bestRank, bestDist := -1, 0, 0
	for _, cand := range titleCandidates(fileTitle) {
		cf := compactTitle(cand)
		for k := range eps {
			if !sameTitle(cand, eps[k].Title) {
				continue
			}
			rank := 1
			if compactTitle(eps[k].Title) == cf {
				rank = 2
			}
			d := eps[k].Episode - near
			if d < 0 {
				d = -d
			}
			if best < 0 || rank > bestRank || rank == bestRank && d < bestDist {
				best, bestRank, bestDist = k, rank, d
			}
		}
		if best >= 0 {
			return best
		}
	}
	return -1
}

// titleAgrees reports whether a provider episode's title can be the file's: the same
// title, or a provider title that names nothing ("", "Episode 3").
func titleAgrees(fileTitle, providerTitle string) bool {
	if providerTitle == "" || reGenericEpisode.MatchString(providerTitle) {
		return true
	}
	for _, c := range titleCandidates(fileTitle) {
		if sameTitle(c, providerTitle) {
			return true
		}
	}
	return false
}

// namesSomething reports whether a file's episode title is a name rather than a label
// ("S1 sp 17.5", "OVA 2"): it has a word of three or more letters that is not one of the
// words such labels are made of.
func namesSomething(t string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(t), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if len([]rune(w)) >= 3 && !labelWords[w] {
			return true
		}
	}
	return false
}

var labelWords = map[string]bool{"ova": true, "oad": true, "ona": true, "special": true, "specials": true, "extra": true, "extras": true,
	"episode": true, "part": true, "season": true, "bonus": true, "recap": true, "preview": true, "picture": true, "drama": true}

var reGenericEpisode = regexp.MustCompile(`(?i)^\s*(?:episode|ep\.?|special)\s*\d+\s*$`)

// storeSeasonIDs records each season's own AniList and MyAnimeList ids. AniList models
// every TV season as its own entry and AniSkip looks skip times up by that entry's MAL id,
// but library seasons follow TMDB's numbering, which can differ from AniList's chain of
// sequels (split cours, arcs). So a season gets the ids only when the two agree on it:
// TMDB lists the same episode count (one off allowed, for an OVA AniList counts in), or,
// with no TMDB season to compare (or TMDB split differently from the folders), every file
// in it fits inside the AniList entry. When the folders split AniList's seasons (see
// layout.go), a season gets the ids of the AniList entry it covers exactly, from its
// first episode, and none otherwise. A season that stops agreeing loses the ids, so it
// never borrows another season's times.
func (s *Service) storeSeasonIDs(ctx context.Context, series *model.Item, primary, secondary *Result, layouts map[*Result]*seasonLayout, lengths map[int]int) {
	var al, other *Result
	for _, r := range []*Result{primary, secondary} {
		switch {
		case r == nil:
		case r.Provider == "anilist":
			al = r
		case other == nil:
			other = r
		}
	}
	if al == nil || len(al.Seasons) == 0 {
		return
	}
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		return
	}
	alLayout := layouts[al]
	for _, se := range seasons {
		if se.IndexAired == nil || *se.IndexAired < 1 {
			continue
		}
		n := *se.IndexAired
		var si SeasonInfo
		trusted := false
		if alLayout != nil && alLayout.split {
			if ps, ok := alLayout.wholeSeason(al, n, lengths[n]); ok && lengths[n] > 0 {
				if p := seasonInfo(al, ps); p != nil && p.ProviderID != "" {
					si, trusted = *p, true
				}
			}
		} else {
			if n > len(al.Seasons) {
				continue
			}
			si = al.Seasons[n-1]
			if si.Number != n || si.ProviderID == "" {
				continue
			}
			otherSplit := other != nil && layouts[other] != nil && layouts[other].split
			if o := seasonInfo(other, n); o != nil && o.EpisodeCount > 0 && !otherSplit {
				d := o.EpisodeCount - si.EpisodeCount
				trusted = si.EpisodeCount > 0 && d >= -1 && d <= 1
			} else {
				maxIdx := 0
				if eps, err := s.store.Children(ctx, se.ID, model.TypeEpisode); err == nil {
					for _, e := range eps {
						if !e.IsVirtual && e.IndexAired != nil && *e.IndexAired > maxIdx {
							maxIdx = *e.IndexAired
						}
					}
				}
				trusted = maxIdx > 0 && si.EpisodeCount > 0 && maxIdx <= si.EpisodeCount
			}
		}
		if !trusted {
			_ = s.store.DeleteProviderIDs(ctx, se.ID, "anilist", "mal")
			continue
		}
		before, _ := s.store.ProviderID(ctx, se.ID, "mal")
		ids := map[string]string{"anilist": si.ProviderID, "mal": si.MalID}
		if err := s.store.SetProviderIDs(ctx, se.ID, ids); err != nil {
			slog.Warn("metadata: season ids", "series", series.Title, "season", n, "err", err)
			continue
		}
		if si.MalID != "" && si.MalID != before && s.OnSeasonMal != nil {
			s.OnSeasonMal(se.ID)
		}
	}
}

func seasonInfo(r *Result, n int) *SeasonInfo {
	if r == nil {
		return nil
	}
	for i := range r.Seasons {
		if r.Seasons[i].Number == n {
			return &r.Seasons[i]
		}
	}
	return nil
}

// createPlaceholders adds virtual seasons/episodes for everything the providers list
// that the library does not have, so the UI can show "missing" and "unaired" entries
// (Jellyfin-style). Regular seasons are always created; Specials (season 0) only when
// the library already has a Specials season with at least one real file, because
// provider specials lists are long and mostly recaps/shorts. Placeholders share the
// GUIDs the scanner uses, so an arriving file flips the same row to real.
//
// The seasons and their lengths come from src alone (placeholderSource: TMDB whenever it
// matched); other only lends titles for episodes src has no data for. An episode a file
// covers in another way (a multi-episode file's range, a title claimed in claimed) gets
// no placeholder.
//
// When the library's seasons split the provider's differently (layout.go: JJK's three
// season folders against TMDB's one season of 59), the folders win: a season gets
// placeholders only for gaps up to its last real episode, the last real season also for
// what the provider has after it in the same provider season, and later provider seasons
// become the seasons after the last real one. Nothing duplicates a real season.
func (s *Service) createPlaceholders(ctx context.Context, series *model.Item, seasons []model.Item, src, other *Result, fetch func(*Result, int) *Season, layouts map[*Result]*seasonLayout, lengths map[int]int, claimed map[int]map[int]bool) {
	if src == nil || len(src.Seasons) == 0 {
		return
	}
	layoutOf := func(r *Result) *seasonLayout {
		if l := layouts[r]; l != nil {
			return l
		}
		return layoutFor(r, lengths)
	}
	lay := layoutOf(src)
	bySeason := map[int]*model.Item{}
	for i := range seasons {
		n := 1
		if seasons[i].IndexAired != nil {
			n = *seasons[i].IndexAired
		}
		bySeason[n] = &seasons[i]
	}
	// episodeAt finds provider data for library (season, episode n) through the layout.
	episodeAt := func(season, n int) *EpisodeResult {
		for _, r := range []*Result{src, other} {
			if r == nil {
				continue
			}
			ps, pe, ok := layoutOf(r).toProvider(r, season, n)
			if !ok {
				continue
			}
			if se := fetch(r, ps); se != nil {
				for k := range se.Episodes {
					if se.Episodes[k].Episode == pe {
						return &se.Episodes[k]
					}
				}
			}
		}
		return nil
	}
	type plan struct {
		number, count int
		poster        string
	}
	var plans []plan
	hasSpecials := func() bool {
		sp, ok := bySeason[0]
		if !ok {
			return false
		}
		eps, err := s.store.Children(ctx, sp.ID, model.TypeEpisode)
		return err == nil && len(eps) > 0
	}
	// seasonCount is a provider season's episode count, or its list's length when longer.
	seasonCount := func(si SeasonInfo) int {
		count := si.EpisodeCount
		if se := fetch(src, si.Number); se != nil && len(se.Episodes) > count {
			count = len(se.Episodes)
		}
		return count
	}
	if lay.split {
		for _, si := range src.Seasons {
			if si.Number == 0 && hasSpecials() {
				plans = append(plans, plan{number: 0, count: seasonCount(si), poster: si.Poster})
			}
		}
		for n := 1; n <= lay.last; n++ {
			count := lengths[n]
			if n == lay.last {
				count += lay.tail
			}
			plans = append(plans, plan{number: n, count: count})
		}
		for n := lay.last + 1; ; n++ {
			ps, ok := lay.beyond[n]
			if !ok {
				break
			}
			si := seasonInfo(src, ps)
			if count := seasonCount(*si); count > 0 {
				plans = append(plans, plan{number: n, count: count, poster: si.Poster})
			}
		}
	} else {
		for _, si := range src.Seasons {
			if si.Number == 0 && !hasSpecials() {
				continue
			}
			count := seasonCount(si)
			if _, ok := bySeason[si.Number]; !ok && count == 0 {
				continue // announced season with no episodes yet: nothing to show
			}
			plans = append(plans, plan{number: si.Number, count: count, poster: si.Poster})
		}
	}

	keep := map[string]bool{}
	created := 0
	for _, pl := range plans {
		seasonGUID := fmt.Sprintf("%s:season:%d", series.GUID, pl.number)
		keep[seasonGUID] = true
		season, ok := bySeason[pl.number]
		if !ok {
			title := fmt.Sprintf("Season %d", pl.number)
			if pl.number == 0 {
				title = "Specials"
			}
			n := pl.number
			it := &model.Item{ID: newID(), GUID: seasonGUID, LibraryID: series.LibraryID, Type: model.TypeSeason, ParentID: &series.ID, Title: title, IndexAired: &n}
			stored, made, err := s.store.EnsureVirtualItem(ctx, it)
			if err != nil {
				slog.Warn("metadata: creating virtual season", "series", series.Title, "season", pl.number, "err", err)
				continue
			}
			if made && pl.poster != "" {
				s.fetchImage(ctx, stored.ID, "poster", pl.poster)
			}
			season = stored
			bySeason[pl.number] = season
		}
		// What the season already holds, by episode number; a file with only an absolute
		// number ("Show - 03.mkv") counts as that episode, as the shadow cleanup does,
		// and a multi-episode file (S01E01-E03) as every episode in its range.
		existing := map[int]bool{}
		if eps, err := s.store.Children(ctx, season.ID, model.TypeEpisode); err == nil {
			for _, e := range eps {
				switch {
				case e.IndexAired != nil:
					existing[*e.IndexAired] = true
					if !e.IsVirtual && e.IndexAiredEnd != nil {
						for n := *e.IndexAired + 1; n <= *e.IndexAiredEnd; n++ {
							existing[n] = true
						}
					}
				case e.IndexAbsolute != nil && !e.IsVirtual:
					existing[*e.IndexAbsolute] = true
				}
			}
		}
		for n := 1; n <= pl.count; n++ {
			guid := fmt.Sprintf("%s:s%02de%04d", series.GUID, pl.number, n)
			if claimed[pl.number][n] {
				continue // a file under another number is this episode (by its title)
			}
			keep[guid] = true
			if existing[n] {
				continue
			}
			hit := episodeAt(pl.number, n)
			sn, en := pl.number, n
			it := &model.Item{ID: newID(), GUID: guid, LibraryID: series.LibraryID, Type: model.TypeEpisode, ParentID: &season.ID, Title: fmt.Sprintf("Episode %d", n), IndexAired: &en, SeasonAired: &sn}
			if hit != nil && hit.Title != "" {
				it.Title = hit.Title
			}
			stored, made, err := s.store.EnsureVirtualItem(ctx, it)
			if err != nil || !made {
				continue
			}
			if hit != nil {
				_ = s.store.UpdateItemMetadata(ctx, stored.ID, store.ItemMetadata{Title: hit.Title, Overview: hit.Overview, Premiered: hit.AirDate})
				if hit.Thumb != "" {
					s.fetchImage(ctx, stored.ID, "thumb", hit.Thumb)
				}
			}
			created++
		}
	}
	if dropped, err := s.store.DeleteVirtualChildren(ctx, series.ID, keep); err == nil && dropped > 0 {
		slog.Info("metadata: stale placeholders removed", "series", series.Title, "count", dropped)
	}
	// A file that arrived under a different GUID (absolute numbering) or covers several
	// episodes now covers its placeholder: drop the placeholder rather than list the
	// episode twice.
	if dropped, err := s.store.DeleteShadowedPlaceholders(ctx, series.ID); err == nil && dropped > 0 {
		slog.Info("metadata: shadowed placeholders removed", "series", series.Title, "count", dropped)
	}
	if created > 0 {
		slog.Info("metadata: placeholders created", "series", series.Title, "count", created)
	}
}

func newID() string { return uuid.NewString() }

// UseFFmpeg lets the service grab frames for episodes that have no still.
func (s *Service) UseFFmpeg(ff *ffmpeg.FFmpeg) { s.ff = ff }

// fillStills grabs a frame for every episode of a series (or the film itself when it has
// neither a thumb nor a backdrop) that still has no thumb. Cheap when nothing is missing.
func (s *Service) fillStills(ctx context.Context, it *model.Item) {
	if s.ff == nil {
		return
	}
	var targets []model.Item
	switch it.Type {
	case model.TypeSeries:
		seasons, err := s.store.Children(ctx, it.ID, model.TypeSeason)
		if err != nil {
			return
		}
		for _, se := range seasons {
			eps, err := s.store.Children(ctx, se.ID, model.TypeEpisode)
			if err != nil {
				return
			}
			targets = append(targets, eps...)
		}
	case model.TypeMovie:
		if b, err := s.store.GetItemImage(ctx, it.ID, "backdrop"); err != nil || b != nil {
			return
		}
		targets = []model.Item{*it}
	default:
		return
	}
	made := 0
	for i := range targets {
		if targets[i].IsVirtual {
			continue
		}
		if img, err := s.store.GetItemImage(ctx, targets[i].ID, "thumb"); err != nil || img != nil {
			continue
		}
		img, err := stills.Grab(ctx, s.ff, s.store, s.cfg, targets[i].ID)
		if err != nil {
			slog.Warn("metadata: frame still failed", "item", targets[i].Title, "err", err)
			continue
		}
		if img != nil {
			made++
		}
	}
	if made > 0 {
		slog.Info("metadata: frame stills made", "item", it.Title, "count", made)
	}
}

// fetchImage downloads an image into the cache and records it: photos (poster, backdrop,
// thumb) as JPEG, logos as PNG so their transparency survives. Already-cached files from
// the same URL in the same format are kept.
func (s *Service) fetchImage(ctx context.Context, itemID, typ, srcURL string) {
	s.fetchImageOpt(ctx, itemID, typ, srcURL, false)
}

// SetImage stores art the admin chose (any https URL) and locks it, so refreshes keep it.
func (s *Service) SetImage(ctx context.Context, itemID, typ, srcURL string) error {
	s.fetchImageOpt(ctx, itemID, typ, srcURL, true)
	img, err := s.store.GetItemImage(ctx, itemID, typ)
	if err != nil {
		return err
	}
	if img == nil || img.SourceURL != srcURL {
		return fmt.Errorf("could not fetch %s", srcURL)
	}
	return nil
}

// readImageSource fetches an image's bytes: over HTTP, or from disk for "file://<path>"
// sources (artwork next to the media: poster.jpg, fanart.jpg, episode thumbs). A local
// source may carry "#<mtime>" so a replaced file counts as a new source.
func (s *Service) readImageSource(ctx context.Context, srcURL string) ([]byte, error) {
	if local, ok := strings.CutPrefix(srcURL, "file://"); ok {
		if i := strings.LastIndex(local, "#"); i > 0 {
			local = local[:i]
		}
		// Artwork symlinked out of the media folders is not read (or cached and served).
		if err := safefs.NoEscape(local); err != nil {
			return nil, err
		}
		f, err := os.Open(local)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, 20<<20))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.images.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// fetchImageOpt downloads one image into the cache. A locked image (admin's choice) is
// kept unless force is set, which is how the admin replaces it.
func (s *Service) fetchImageOpt(ctx context.Context, itemID, typ, srcURL string, force bool) {
	dir := filepath.Join(s.cfg.ImagesDir(), itemID)
	ext := ".jpg"
	if typ == "logo" {
		ext = ".png"
	}
	path := filepath.Join(dir, typ+ext)
	if existing, err := s.store.GetItemImage(ctx, itemID, typ); err == nil && existing != nil {
		if existing.Locked && !force {
			return
		}
		if existing.SourceURL == srcURL && filepath.Ext(existing.Path) == ext && !s.replaceImages {
			if _, err := os.Stat(existing.Path); err == nil {
				return
			}
		}
	}
	data, err := s.readImageSource(ctx, srcURL)
	if err != nil {
		slog.Warn("metadata: image download failed", "url", srcURL, "err", err)
		return
	}
	cfg, format, err := imaging.CheckConfig(data) // refuses decompression bombs before decoding
	if err != nil {
		slog.Warn("metadata: undecodable image", "url", srcURL, "err", err)
		return
	}
	want := "jpeg"
	if ext == ".png" {
		want = "png"
	}
	if format != want {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			slog.Warn("metadata: undecodable image", "url", srcURL, "err", err)
			return
		}
		var buf bytes.Buffer
		if want == "png" {
			if err := png.Encode(&buf, img); err != nil {
				return
			}
		} else if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return
		}
		data = buf.Bytes()
	}
	if err := safefs.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("metadata: image cache dir", "err", err)
		return
	}
	// A unique temp file: two refreshes of the same item can save the same image at once.
	if err := safefs.WriteFileAtomic(path, data, 0o644); err != nil {
		return
	}
	dropResizedVariants(dir, typ)
	if err := s.store.SetItemImage(ctx, store.ItemImage{ItemID: itemID, Type: typ, Path: path, Width: cfg.Width, Height: cfg.Height, SourceURL: srcURL, Locked: force}); err != nil {
		slog.Warn("metadata: recording image", "item", itemID, "type", typ, "err", err)
	}
}

// dropResizedVariants removes the resized copies (<type>.w<px>.jpg, or .png for logos,
// which keep their transparency) made from the previous image of that type. Only .jpg
// used to be removed, so a new logo kept being served at every width a client had
// already asked for.
func dropResizedVariants(dir, typ string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		rest, ok := strings.CutPrefix(n, typ+".w")
		if !ok {
			continue
		}
		digits, ext, ok := strings.Cut(rest, ".")
		if !ok || (ext != "jpg" && ext != "png") || digits == "" || strings.Trim(digits, "0123456789") != "" {
			continue
		}
		safefs.Remove(filepath.Join(dir, n))
	}
}
