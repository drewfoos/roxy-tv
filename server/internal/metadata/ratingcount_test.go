package metadata

import (
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The count belongs to the rating it came with: the secondary's rating brings its own
// count, and the primary's rating is never paired with the secondary's count.
func TestMergeKeepsTheCountWithItsRating(t *testing.T) {
	a := &Result{CommunityRating: 8.1, RatingCount: 0}
	b := &Result{CommunityRating: 7.5, RatingCount: 900}
	if got := merge(a, b); got.CommunityRating != 8.1 || got.RatingCount != 0 {
		t.Errorf("primary's rating with the other's count: %v (%d)", got.CommunityRating, got.RatingCount)
	}
	if got := merge(&Result{}, b); got.CommunityRating != 7.5 || got.RatingCount != 900 {
		t.Errorf("secondary's rating lost its count: %v (%d)", got.CommunityRating, got.RatingCount)
	}
}

// A rated title is looked up again once for its count, only when its last lookup was
// before counts were kept.
func TestNeedsRatingCount(t *testing.T) {
	r, n := 8.0, 12
	before, after := ratingCountSince.Add(-time.Hour), ratingCountSince.Add(time.Hour)
	if !needsRatingCount(&model.Item{CommunityRating: &r, MetadataRefreshedAt: &before}) {
		t.Error("an old lookup with a rating and no count should be redone")
	}
	for name, it := range map[string]*model.Item{
		"has a count":     {CommunityRating: &r, RatingCount: &n, MetadataRefreshedAt: &before},
		"looked up since": {CommunityRating: &r, MetadataRefreshedAt: &after},
		"no rating":       {MetadataRefreshedAt: &before},
		"never looked up": {CommunityRating: &r},
	} {
		if needsRatingCount(it) {
			t.Errorf("%s: should not be looked up again", name)
		}
	}
}

// Saved with the rating; a later rating without a count clears the old one; a lookup
// with no rating at all leaves both.
func TestRatingCountIsSavedWithTheRating(t *testing.T) {
	f := newFixture(t, false)
	count := func() (float64, int) {
		it, err := f.st.GetItem(f.ctx, f.series.ID)
		if err != nil || it == nil {
			t.Fatal(err)
		}
		r, n := 0.0, -1
		if it.CommunityRating != nil {
			r = *it.CommunityRating
		}
		if it.RatingCount != nil {
			n = *it.RatingCount
		}
		return r, n
	}
	save := func(m store.ItemMetadata) {
		if err := f.st.UpdateItemMetadata(f.ctx, f.series.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	save(store.ItemMetadata{CommunityRating: 8.1, RatingCount: 4200})
	if r, n := count(); r != 8.1 || n != 4200 {
		t.Fatalf("saved: %v (%d)", r, n)
	}
	save(store.ItemMetadata{})
	if r, n := count(); r != 8.1 || n != 4200 {
		t.Errorf("a lookup with no rating changed them: %v (%d)", r, n)
	}
	save(store.ItemMetadata{CommunityRating: 7.0})
	if r, n := count(); r != 7.0 || n != -1 {
		t.Errorf("a new rating kept the old rating's count: %v (%d)", r, n)
	}
}
