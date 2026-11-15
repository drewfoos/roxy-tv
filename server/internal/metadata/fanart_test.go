package metadata

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// fakeFanart serves one series' art the way fanart.tv does and counts the asks. The
// images' URLs point back at it, so a fetch of one is served too.
type fakeFanart struct {
	srv  *httptest.Server
	asks atomic.Int32
}

func newFakeFanart(t *testing.T) *fakeFanart {
	t.Helper()
	f := &fakeFanart{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			var buf bytes.Buffer
			_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 4)))
			_, _ = w.Write(buf.Bytes())
			return
		}
		f.asks.Add(1)
		if r.URL.Path != "/tv/555" || r.URL.Query().Get("api_key") != "k" {
			http.NotFound(w, r)
			return
		}
		u := f.srv.URL + "/img/"
		_, _ = w.Write([]byte(`{
			"hdtvlogo":  [{"url":"` + u + `logo-ja.png","lang":"ja","likes":"9"}],
			"clearlogo": [{"url":"` + u + `logo-en-1.png","lang":"en","likes":"1"},{"url":"` + u + `logo-en-5.png","lang":"en","likes":"5"}],
			"showbackground": [{"url":"` + u + `bg-en.jpg","lang":"en","likes":"7"},{"url":"` + u + `bg-none.jpg","lang":"00","likes":"2"}],
			"seasonposter": [{"url":"` + u + `s1-ja.jpg","lang":"ja","likes":"9","season":"1"},{"url":"` + u + `s1-en.jpg","lang":"en","likes":"1","season":"1"},{"url":"` + u + `s2.jpg","lang":"en","likes":"1","season":"2"},{"url":"` + u + `all.jpg","lang":"en","likes":"9","season":"all"}],
			"tvposter": [{"url":"` + u + `poster-none.jpg","lang":"00","likes":"8"},{"url":"` + u + `poster-en.jpg","lang":"en","likes":"3"}]
		}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFanart) client() *Fanart {
	c := NewFanart("k")
	c.base = f.srv.URL + "/"
	return c
}

// An English logo beats a better-liked Japanese HD one, the best-liked wins within a
// language, a background without words beats one with, and the textless poster ("hero")
// is only ever a poster with no language.
func TestFanartBestPicks(t *testing.T) {
	ff := newFakeFanart(t)
	got, err := ff.client().Best(context.Background(), false, "555")
	if err != nil {
		t.Fatal(err)
	}
	u := ff.srv.URL + "/img/"
	want := map[string]string{"logo": u + "logo-en-5.png", "backdrop": u + "bg-none.jpg", "poster": u + "poster-en.jpg", "hero": u + "poster-none.jpg"}
	for typ, w := range want {
		if got[typ] != w {
			t.Errorf("%s: got %q, want %q", typ, got[typ], w)
		}
	}
	// A title fanart.tv does not know: nothing, and not an error.
	if got, err := ff.client().Best(context.Background(), false, "1"); err != nil || len(got) != 0 {
		t.Errorf("unknown title: got %v, %v", got, err)
	}
}

func fanartFixture(t *testing.T) (*svcFixture, *fakeFanart) {
	t.Helper()
	f := newFixture(t, false)
	ff := newFakeFanart(t)
	data := t.TempDir()
	safefs.Init(data)
	f.svc.cfg = &config.Config{DataDir: data}
	f.svc.images = &http.Client{Timeout: 5 * time.Second}
	f.svc.fanartBase = ff.srv.URL + "/"
	if err := f.st.SetSetting(f.ctx, "fanart", map[string]string{"apiKey": "k"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ReplaceProviderIDs(f.ctx, f.series.ID, map[string]string{"tvdb": "555"}); err != nil {
		t.Fatal(err)
	}
	it, err := f.st.GetItem(f.ctx, f.series.ID)
	if err != nil || it == nil {
		t.Fatalf("series: %v", err)
	}
	f.series = it
	return f, ff
}

// A lookup's result keeps what the providers gave and takes fanart.tv's only for the
// kinds they gave nothing for; AniList's banner strip as the backdrop counts as nothing.
func TestFanartFillsOnlyWhatIsMissing(t *testing.T) {
	f, ff := fanartFixture(t)
	u := ff.srv.URL + "/img/"
	merged := &Result{ProviderIDs: map[string]string{}, Images: map[string]string{
		"poster":   "https://image.tmdb.org/t/p/original/p.jpg",
		"backdrop": "https://s4.anilist.co/file/anilistcdn/media/anime/banner/1.jpg",
	}}
	f.svc.fillFromFanart(f.ctx, f.series, merged)
	want := map[string]string{"poster": "https://image.tmdb.org/t/p/original/p.jpg", "backdrop": u + "bg-none.jpg", "logo": u + "logo-en-5.png", "hero": u + "poster-none.jpg"}
	for typ, w := range want {
		if merged.Images[typ] != w {
			t.Errorf("%s: got %q, want %q", typ, merged.Images[typ], w)
		}
	}
	if n := ff.asks.Load(); n != 1 {
		t.Errorf("fanart.tv was asked %d times for one title, want 1", n)
	}

	// Nothing missing: fanart.tv is not asked at all.
	full := &Result{Images: map[string]string{"poster": "a", "backdrop": "b", "logo": "c", "hero": "d"}}
	f.svc.fillFromFanart(f.ctx, f.series, full)
	if n := ff.asks.Load(); n != 1 {
		t.Errorf("fanart.tv was asked with nothing missing (%d asks)", n)
	}

	// No key: nothing is asked and nothing changes.
	if err := f.st.SetSetting(f.ctx, "fanart", map[string]string{"apiKey": ""}); err != nil {
		t.Fatal(err)
	}
	bare := &Result{Images: map[string]string{}}
	f.svc.fillFromFanart(f.ctx, f.series, bare)
	if len(bare.Images) != 0 || ff.asks.Load() != 1 {
		t.Errorf("without a key: images %v, asks %d", bare.Images, ff.asks.Load())
	}
}

// The sweep fills a stored title's missing art and leaves the art it has alone, and it
// runs once per key: a second start does not ask about the whole library again.
func TestFanartSweepFillsStoredTitlesOnce(t *testing.T) {
	f, ff := fanartFixture(t)
	if err := f.st.SetItemImage(f.ctx, store.ItemImage{ItemID: f.series.ID, Type: "poster", Path: "/nowhere/poster.jpg", SourceURL: "https://image.tmdb.org/t/p/original/p.jpg"}); err != nil {
		t.Fatal(err)
	}
	drain := func() int {
		n := 0
		for {
			select {
			case j := <-f.svc.queue:
				delete(f.svc.pending, j.key())
				f.svc.processSafe(f.ctx, j)
				n++
			default:
				return n
			}
		}
	}
	f.svc.SweepArt(f.ctx)
	if n := drain(); n != 1 {
		t.Fatalf("the sweep queued %d titles, want 1", n)
	}
	for _, typ := range []string{"logo", "backdrop", "hero"} {
		img, err := f.st.GetItemImage(f.ctx, f.series.ID, typ)
		if err != nil || img == nil || !strings.HasPrefix(img.SourceURL, ff.srv.URL) {
			t.Errorf("%s was not filled from fanart.tv: %+v, %v", typ, img, err)
		}
	}
	if img, _ := f.st.GetItemImage(f.ctx, f.series.ID, "poster"); img == nil || !strings.Contains(img.SourceURL, "tmdb") {
		t.Errorf("the poster the title had was replaced: %+v", img)
	}
	f.svc.SweepArt(f.ctx)
	if n := drain(); n != 0 {
		t.Errorf("a second sweep with the same key queued %d titles", n)
	}
}

// Seasons without a poster get fanart.tv's for their number (English first); a season
// that has one keeps it, and a series whose seasons all have posters is not asked about.
func TestFanartSeasonPosters(t *testing.T) {
	f, ff := fanartFixture(t)
	f.realSeason(1, 1)
	f.realSeason(2, 1)
	f.realSeason(3, 1) // fanart.tv has none for it
	if err := f.st.SetItemImage(f.ctx, store.ItemImage{ItemID: "season2", Type: "poster", Path: "/nowhere/s2.jpg", SourceURL: "https://image.tmdb.org/t/p/original/s2.jpg"}); err != nil {
		t.Fatal(err)
	}
	f.svc.fillSeasonPosters(f.ctx, f.series, "555")
	if img, _ := f.st.GetItemImage(f.ctx, "season1", "poster"); img == nil || img.SourceURL != ff.srv.URL+"/img/s1-en.jpg" {
		t.Errorf("season 1: %+v", img)
	}
	if img, _ := f.st.GetItemImage(f.ctx, "season2", "poster"); img == nil || !strings.Contains(img.SourceURL, "tmdb") {
		t.Errorf("season 2's own poster was replaced: %+v", img)
	}
	if img, _ := f.st.GetItemImage(f.ctx, "season3", "poster"); img != nil {
		t.Errorf("season 3 got a poster fanart.tv does not have: %+v", img)
	}
	asks := ff.asks.Load()
	if err := f.st.SetItemImage(f.ctx, store.ItemImage{ItemID: "season3", Type: "poster", Path: "/nowhere/s3.jpg", SourceURL: "x"}); err != nil {
		t.Fatal(err)
	}
	f.svc.fillSeasonPosters(f.ctx, f.series, "555")
	if ff.asks.Load() != asks {
		t.Errorf("fanart.tv was asked although every season has a poster")
	}
}
