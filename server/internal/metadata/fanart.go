package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// fanart.tv as the fallback for art: whatever kind of picture the lookup found nowhere
// else (TMDB, AniList) is taken from fanart.tv when an admin has entered a key. It never
// replaces art another provider gave, art next to the media or art picked by hand; the
// one exception is AniList's banner strip standing in as the backdrop, which a real
// 16:9 background beats.

// fanartKinds are the kinds of art fanart.tv can fill in for a series or a film.
// Seasons get their posters from it too (fillSeasonPosters).
var fanartKinds = []string{"poster", "backdrop", "logo", "hero"}

const anilistBanner = "anilistcdn/media/anime/banner"

// fanart is the client for the key in settings, or nil without one.
func (s *Service) fanart(ctx context.Context) *Fanart {
	var cfg struct {
		APIKey string `json:"apiKey"`
	}
	if ok, err := s.store.GetSetting(ctx, "fanart", &cfg); err != nil || !ok {
		return nil
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fanartClient == nil || s.fanartClient.key != key {
		s.fanartClient = NewFanart(key)
		if s.fanartBase != "" {
			s.fanartClient.base = s.fanartBase
		}
	}
	return s.fanartClient
}

// fanartArt asks fanart.tv for the kinds in missing: kind -> URL for those it has.
// Nothing is asked when nothing is missing, there is no key, or the title has no id
// fanart.tv knows it by (TVDB for a series, TMDB for a film). A failed ask is logged and
// fills nothing: the next refresh asks again.
func (s *Service) fanartArt(ctx context.Context, it *model.Item, ids map[string]string, missing []string) map[string]string {
	if len(missing) == 0 {
		return nil
	}
	f := s.fanart(ctx)
	if f == nil {
		return nil
	}
	isMovie := it.Type == model.TypeMovie
	id := ids["tvdb"]
	if isMovie {
		id = ids["tmdb"]
	}
	if id == "" {
		return nil
	}
	best, err := f.Best(ctx, isMovie, id)
	if err != nil {
		slog.Warn("metadata: fanart.tv did not answer", "item", it.Title, "err", err)
		return nil
	}
	out := map[string]string{}
	for _, typ := range missing {
		if u := best[typ]; u != "" {
			out[typ] = u
		}
	}
	return out
}

// fillFromFanart adds fanart.tv's art to a lookup's result for the kinds no provider
// gave, before the images are fetched.
func (s *Service) fillFromFanart(ctx context.Context, it *model.Item, merged *Result) {
	var missing []string
	for _, typ := range fanartKinds {
		if u := merged.Images[typ]; u == "" || (typ == "backdrop" && strings.Contains(u, anilistBanner)) {
			missing = append(missing, typ)
		}
	}
	ids := map[string]string{}
	for k, v := range merged.ProviderIDs {
		ids[k] = v
	}
	for k, v := range it.ProviderIDs {
		if v != "" {
			ids[k] = v
		}
	}
	art := s.fanartArt(ctx, it, ids, missing)
	if len(art) == 0 {
		return
	}
	if merged.Images == nil {
		merged.Images = map[string]string{}
	}
	kinds := make([]string, 0, len(art))
	for typ, u := range art {
		merged.Images[typ] = u
		kinds = append(kinds, typ)
	}
	slog.Info("metadata: art from fanart.tv", "item", it.Title, "kinds", kinds)
}

// fillArt fills one stored title's missing art from fanart.tv, without a lookup: for
// the library as it is when a key is first entered (SweepArt).
func (s *Service) fillArt(ctx context.Context, itemID string) {
	it, err := s.store.GetItem(ctx, itemID)
	if err != nil || it == nil || (it.Type != model.TypeSeries && it.Type != model.TypeMovie) {
		return
	}
	if locked, err := s.store.MetadataLocked(ctx, it.ID); err != nil || locked {
		return
	}
	var missing []string
	for _, typ := range fanartKinds {
		img, err := s.store.GetItemImage(ctx, it.ID, typ)
		if err != nil {
			continue
		}
		if img == nil || (typ == "backdrop" && !img.Locked && strings.Contains(img.SourceURL, anilistBanner)) {
			missing = append(missing, typ)
		}
	}
	art := s.fanartArt(ctx, it, it.ProviderIDs, missing)
	kinds := make([]string, 0, len(art))
	for typ, u := range art {
		s.fetchImage(ctx, it.ID, typ, u)
		kinds = append(kinds, typ)
	}
	if len(kinds) > 0 {
		slog.Info("metadata: art from fanart.tv", "item", it.Title, "kinds", kinds)
	}
	s.fillSeasonPosters(ctx, it, it.ProviderIDs["tvdb"])
}

// fillSeasonPosters gives a series' seasons that have no poster fanart.tv's, by season
// number. Roxy numbers seasons as TMDB does, fanart.tv as TheTVDB does: the same for
// nearly every show; where they differ a season can get a neighbour's poster, which the
// art picker or a poster from TMDB (it wins at any refresh) puts right. Nothing is asked
// when every season has a poster.
func (s *Service) fillSeasonPosters(ctx context.Context, series *model.Item, tvdbID string) {
	if series.Type != model.TypeSeries || tvdbID == "" {
		return
	}
	seasons, err := s.store.Children(ctx, series.ID, model.TypeSeason)
	if err != nil {
		return
	}
	var bare []model.Item
	for _, se := range seasons {
		if se.IndexAired == nil {
			continue
		}
		if img, err := s.store.GetItemImage(ctx, se.ID, "poster"); err == nil && img == nil {
			bare = append(bare, se)
		}
	}
	if len(bare) == 0 {
		return
	}
	f := s.fanart(ctx)
	if f == nil {
		return
	}
	posters, err := f.SeasonPosters(ctx, tvdbID)
	if err != nil {
		slog.Warn("metadata: fanart.tv did not answer", "item", series.Title, "err", err)
		return
	}
	var filled []int
	for _, se := range bare {
		if u := posters[*se.IndexAired]; u != "" {
			s.fetchImage(ctx, se.ID, "poster", u)
			filled = append(filled, *se.IndexAired)
		}
	}
	if len(filled) > 0 {
		slog.Info("metadata: season posters from fanart.tv", "item", series.Title, "seasons", filled)
	}
}

// SweepArt queues every series and film for fillArt, once per fanart.tv key: at start
// and when an admin saves a key. After that the weekly refresh keeps art filled in
// (fillFromFanart), so the library is not asked about again on every start.
func (s *Service) SweepArt(ctx context.Context) {
	f := s.fanart(ctx)
	if f == nil {
		return
	}
	sum := sha256.Sum256([]byte(f.key))
	// The number is what the sweep covers: raise it when it learns to fill something new
	// (2: season posters), so the library is gone over once more.
	mark := "2:" + hex.EncodeToString(sum[:6])
	var done string
	if _, err := s.store.GetSetting(ctx, "fanart:swept", &done); err == nil && done == mark {
		return
	}
	ids, err := s.store.TopLevelItemIDs(ctx)
	if err != nil {
		slog.Warn("metadata: fanart.tv sweep", "err", err)
		return
	}
	if err := s.store.SetSetting(ctx, "fanart:swept", mark); err != nil {
		slog.Warn("metadata: fanart.tv sweep", "err", err)
		return
	}
	for _, id := range ids {
		s.enqueueJob(job{itemID: id, art: true})
	}
	slog.Info("metadata: filling missing art from fanart.tv", "titles", len(ids))
}
