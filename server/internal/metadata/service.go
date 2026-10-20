package metadata

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png" // AniList covers are sometimes PNG (re-encoded to JPEG); logos are kept as PNG
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

const (
	// refreshInterval is how long a successful lookup stays fresh.
	refreshInterval = 7 * 24 * time.Hour
	// retryFailed is how long an unmatched item is left alone (in-process) before the
	// next scan may try it again.
	retryFailed = time.Hour
	queueSize   = 1024
)

// Service runs metadata lookups off a work queue: one worker goroutine, items by id.
type Service struct {
	store     *store.Store
	cfg       *config.Config
	providers []Provider // in default preference order (TMDB first when configured)
	anilist   Provider
	images    *http.Client

	queue   chan job
	force   bool // the job being processed was a forced refresh (worker-local)
	mu      sync.Mutex
	pending map[string]bool      // queued but not yet picked up (dedupe)
	failed  map[string]time.Time // top-level item id -> last unmatched attempt
}

type job struct {
	itemID string
	force  bool
}

// TMDB exposes the TMDB client when configured (Discover), else nil.
func (s *Service) TMDB() *TMDB {
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
		store:   st,
		cfg:     cfg,
		images:  &http.Client{Timeout: 30 * time.Second},
		queue:   make(chan job, queueSize),
		pending: map[string]bool{},
		failed:  map[string]time.Time{},
	}
	s.anilist = NewAniList()
	if cfg.TMDBAPIKey != "" {
		s.providers = append(s.providers, NewTMDB(cfg.TMDBAPIKey))
	} else {
		slog.Warn("metadata: TMDB disabled (ROXY_TMDB_API_KEY not set); AniList only")
	}
	s.providers = append(s.providers, s.anilist)
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

func (s *Service) enqueue(itemID string, force bool) {
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
	case s.queue <- job{itemID: itemID, force: force}:
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
	s.force = j.force
	onlyNewEpisodes := false
	if !j.force {
		if it.MetadataRefreshedAt != nil && time.Since(*it.MetadataRefreshedAt) < refreshInterval {
			if it.Type != model.TypeSeries {
				return nil
			}
			n, err := s.store.CountUnrefreshedEpisodes(ctx, it.ID)
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
	}

	hints := Hints{}
	if libs, err := s.store.ListLibraries(ctx); err == nil {
		for _, l := range libs {
			if l.ID == it.LibraryID {
				hints.Anime = l.AnimeMode
			}
		}
	}

	primary, secondary := s.match(ctx, it, hints)
	if primary == nil {
		s.mu.Lock()
		s.failed[it.ID] = time.Now()
		s.mu.Unlock()
		slog.Info("metadata: no match", "item", it.Title, "type", it.Type, "anime", hints.Anime)
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
	if it.Type == model.TypeSeries {
		n := s.applySeries(ctx, it, primary, secondary, onlyNewEpisodes)
		if onlyNewEpisodes && n == 0 {
			// Nothing the providers know about yet; back off instead of retrying every scan.
			s.mu.Lock()
			s.failed[it.ID] = time.Now()
			s.mu.Unlock()
		}
	}
	return nil
}

// match tries every provider: the first hit is primary (TMDB unless the library is in
// anime mode, where AniList leads), later hits enrich. AniList is always consulted so
// anime gets its anilist id even when TMDB matched first.
func (s *Service) match(ctx context.Context, it *model.Item, hints Hints) (primary, secondary *Result) {
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
			slog.Warn("metadata: provider error", "provider", p.Name(), "item", it.Title, "err", err)
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
	return primary, secondary
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
func (s *Service) applySeries(ctx context.Context, series *model.Item, primary, secondary *Result, onlyNew bool) int {
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		slog.Warn("metadata: listing seasons", "series", series.Title, "err", err)
		return 0
	}
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
	updated := 0
	for i := range seasons {
		season := &seasons[i]
		num := 1
		if season.IndexAired != nil {
			num = *season.IndexAired
		}
		if _, ok := season.Images["poster"]; !ok || !onlyNew {
			if se := fetch(primary, num); se != nil && se.Poster != "" {
				s.fetchImage(ctx, season.ID, "poster", se.Poster)
			} else if se := fetch(secondary, num); se != nil && se.Poster != "" {
				s.fetchImage(ctx, season.ID, "poster", se.Poster)
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
		for j := range episodes {
			ep := &episodes[j]
			if onlyNew && ep.MetadataRefreshedAt != nil {
				continue
			}
			sn, en := num, 0
			if ep.IndexAired != nil {
				en = *ep.IndexAired
			}
			// Absolute-numbered files ("Show - 013 - Title") land in season 1 with
			// index == absolute; map them through the provider's season lengths.
			if ep.IndexAbsolute != nil && (ep.IndexAired == nil || *ep.IndexAired == *ep.IndexAbsolute) {
				if s2, e2, ok := primary.AbsoluteToAired(*ep.IndexAbsolute); ok {
					sn, en = s2, e2
				} else if secondary != nil {
					if s2, e2, ok := secondary.AbsoluteToAired(*ep.IndexAbsolute); ok {
						sn, en = s2, e2
					}
				}
			}
			if en == 0 {
				continue
			}
			var hit *EpisodeResult
			for _, r := range []*Result{primary, secondary} {
				if se := fetch(r, sn); se != nil {
					for k := range se.Episodes {
						if se.Episodes[k].Episode == en {
							hit = &se.Episodes[k]
							break
						}
					}
				}
				if hit != nil {
					break
				}
			}
			if hit == nil {
				slog.Debug("metadata: no episode data", "series", series.Title, "season", sn, "episode", en)
				continue
			}
			meta := store.ItemMetadata{Title: hit.Title, Overview: hit.Overview, Premiered: hit.AirDate}
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
	s.createPlaceholders(ctx, series, seasons, primary, secondary, fetch)
	return updated
}

// createPlaceholders adds virtual seasons/episodes for everything the providers list
// that the library does not have, so the UI can show "missing" and "unaired" entries
// (Jellyfin-style). Regular seasons are always created; Specials (season 0) only when
// the library already has a Specials season with at least one real file, because
// provider specials lists are long and mostly recaps/shorts. Placeholders share the
// GUIDs the scanner uses, so an arriving file flips the same row to real.
func (s *Service) createPlaceholders(ctx context.Context, series *model.Item, seasons []model.Item, primary, secondary *Result, fetch func(*Result, int) *Season) {
	src := primary
	if len(src.Seasons) == 0 && secondary != nil && len(secondary.Seasons) > 0 {
		src = secondary
	}
	if len(src.Seasons) == 0 {
		return
	}
	bySeason := map[int]*model.Item{}
	for i := range seasons {
		n := 1
		if seasons[i].IndexAired != nil {
			n = *seasons[i].IndexAired
		}
		bySeason[n] = &seasons[i]
	}
	keep := map[string]bool{}
	created := 0
	for _, si := range src.Seasons {
		if si.Number == 0 {
			sp, ok := bySeason[0]
			if !ok {
				continue
			}
			if eps, err := s.store.Children(ctx, sp.ID, model.TypeEpisode); err != nil || len(eps) == 0 {
				continue
			}
		}
		se := fetch(src, si.Number)
		if se == nil && src != secondary && secondary != nil {
			se = fetch(secondary, si.Number)
		}
		seasonGUID := fmt.Sprintf("%s:season:%d", series.GUID, si.Number)
		season, ok := bySeason[si.Number]
		if !ok && si.EpisodeCount == 0 && (se == nil || len(se.Episodes) == 0) {
			continue // announced season with no episodes yet: nothing to show
		}
		keep[seasonGUID] = true
		if !ok {
			title := fmt.Sprintf("Season %d", si.Number)
			if si.Number == 0 {
				title = "Specials"
			}
			n := si.Number
			it := &model.Item{ID: newID(), GUID: seasonGUID, LibraryID: series.LibraryID, Type: model.TypeSeason, ParentID: &series.ID, Title: title, IndexAired: &n}
			stored, made, err := s.store.EnsureVirtualItem(ctx, it)
			if err != nil {
				slog.Warn("metadata: creating virtual season", "series", series.Title, "season", si.Number, "err", err)
				continue
			}
			if made && si.Poster != "" {
				s.fetchImage(ctx, stored.ID, "poster", si.Poster)
			}
			season = stored
			bySeason[si.Number] = season
		}
		existing := map[int]bool{}
		if eps, err := s.store.Children(ctx, season.ID, model.TypeEpisode); err == nil {
			for _, e := range eps {
				if e.IndexAired != nil {
					existing[*e.IndexAired] = true
				}
			}
		}
		count := si.EpisodeCount
		if se != nil && len(se.Episodes) > count {
			count = len(se.Episodes)
		}
		for n := 1; n <= count; n++ {
			guid := fmt.Sprintf("%s:s%02de%04d", series.GUID, si.Number, n)
			keep[guid] = true
			if existing[n] {
				continue
			}
			var hit *EpisodeResult
			if se != nil {
				for k := range se.Episodes {
					if se.Episodes[k].Episode == n {
						hit = &se.Episodes[k]
						break
					}
				}
			}
			sn, en := si.Number, n
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
	if created > 0 {
		slog.Info("metadata: placeholders created", "series", series.Title, "count", created)
	}
}

func newID() string { return uuid.NewString() }

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
		if existing.SourceURL == srcURL && filepath.Ext(existing.Path) == ext {
			if _, err := os.Stat(existing.Path); err == nil {
				return
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.images.Do(req)
	if err != nil {
		slog.Warn("metadata: image download failed", "url", srcURL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("metadata: image download failed", "url", srcURL, "status", resp.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("metadata: image cache dir", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return
	}
	// Drop stale resized variants (<type>.w<px>.jpg) of the previous image.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if n := e.Name(); strings.HasPrefix(n, typ+".w") && strings.HasSuffix(n, ".jpg") {
				os.Remove(filepath.Join(dir, n))
			}
		}
	}
	if err := s.store.SetItemImage(ctx, store.ItemImage{ItemID: itemID, Type: typ, Path: path, Width: cfg.Width, Height: cfg.Height, SourceURL: srcURL, Locked: force}); err != nil {
		slog.Warn("metadata: recording image", "item", itemID, "type", typ, "err", err)
	}
}
