package library

import (
	"context"
	"sort"

	"github.com/roxy-media/roxy/server/internal/model"
)

// EpisodeHint is the season and episode number(s) an arr app assigned one file, from
// its webhook. Sonarr has already matched the file (sometimes after a person checked it
// in a manual import), so when the filename carries no usable episode number Roxy trusts
// Sonarr instead of falling back to the bare filename. A name the parser can read always
// wins: a later scan has no hint and must reach the same item.
type EpisodeHint struct {
	Season   int
	Episodes []int // ascending; more than one for a multi-episode file
}

// NewEpisodeHint builds a hint from (season, episode) pairs, or nil when there are none
// or they span more than one season (that cannot describe a single file).
func NewEpisodeHint(pairs [][2]int) *EpisodeHint {
	if len(pairs) == 0 {
		return nil
	}
	h := &EpisodeHint{Season: pairs[0][0]}
	for _, p := range pairs {
		if p[0] != h.Season || p[1] <= 0 {
			return nil
		}
		h.Episodes = append(h.Episodes, p[1])
	}
	sort.Ints(h.Episodes)
	return h
}

func (h *EpisodeHint) valid() bool { return h != nil && h.Season >= 0 && len(h.Episodes) > 0 }

func (p *ParsedName) applyHint(h *EpisodeHint) {
	season, first, last := h.Season, h.Episodes[0], h.Episodes[len(h.Episodes)-1]
	p.Season, p.Episode = &season, &first
	if last > first {
		p.EpisodeEnd = &last
	}
}

// HintForPath returns the season/episode(s) of the episode item that currently owns the
// file at path, or nil. Used to carry numbers across a rename, and by rescans.
func (l *Library) HintForPath(ctx context.Context, path string) *EpisodeHint {
	src, err := l.store.GetSource(ctx, stableID("source", path))
	if err != nil || src == nil {
		return nil
	}
	it, err := l.store.GetItem(ctx, src.ItemID)
	if err != nil || it == nil || it.Type != model.TypeEpisode || it.IndexAired == nil || it.SeasonAired == nil {
		return nil
	}
	h := &EpisodeHint{Season: *it.SeasonAired, Episodes: []int{*it.IndexAired}}
	if it.IndexAiredEnd != nil && *it.IndexAiredEnd > *it.IndexAired {
		h.Episodes = append(h.Episodes, *it.IndexAiredEnd)
	}
	return h
}

func (l *Library) episodeHintFor(ctx context.Context, path string, hint *EpisodeHint) *EpisodeHint {
	if hint.valid() {
		return hint
	}
	if h := l.HintForPath(ctx, path); h.valid() {
		return h
	}
	return nil
}
