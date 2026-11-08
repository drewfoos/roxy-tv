// Command roxyseed fills a fresh data directory with a synthetic show for performance
// checks on real devices: one series of -episodes episodes (seasons of -season, 100 by
// default; -season 5000 puts a 5000-episode list with sources on one screen), each with a
// media source of one video, two audio and two subtitle tracks, as a long anime's files
// look. The files do not exist; the library's folder does not either, which the empty-
// library guard treats as an unmounted drive, so a scan leaves the records alone.
//
//	go run ./cmd/roxyseed -data /tmp/seed -episodes 5000
//	ROXY_DATA_DIR=/tmp/seed ROXY_MEDIA_ROOTS=/seed ROXY_SCAN_ON_START=false roxy
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

func main() {
	data := flag.String("data", "", "data directory (created)")
	n := flag.Int("episodes", 5000, "episodes in the show")
	root := flag.String("root", "/seed", "the library's folder (need not exist)")
	per := flag.Int("season", 100, "episodes per season")
	flag.Parse()
	if *data == "" {
		log.Fatal("-data is required")
	}
	if err := os.MkdirAll(*data, 0o755); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(filepath.Join(*data, "roxy.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	lib := &model.Library{Name: "Seed", Kind: model.KindSeries, Root: *root, AnimeMode: true}
	if err := st.CreateLibrary(ctx, lib); err != nil {
		log.Fatal(err)
	}
	show := "seed-show"
	must(st.UpsertItem(ctx, &model.Item{ID: show, GUID: "seed:show", LibraryID: lib.ID, Type: model.TypeSeries, Title: fmt.Sprintf("Seed Show (%d episodes)", *n)}))
	rt := int64(24 * 60 * 1000)
	for i := 0; i < *n; i++ {
		seasonNo, epNo := i/(*per)+1, i%(*per)+1
		season := fmt.Sprintf("seed-s%03d", seasonNo)
		if epNo == 1 {
			sn := seasonNo
			must(st.UpsertItem(ctx, &model.Item{ID: season, GUID: "seed:" + season, LibraryID: lib.ID, Type: model.TypeSeason, ParentID: &show, IndexAired: &sn, Title: fmt.Sprintf("Season %d", sn)}))
		}
		id := fmt.Sprintf("seed-e%05d", i+1)
		sn, en := seasonNo, epNo
		must(st.UpsertItem(ctx, &model.Item{ID: id, GUID: "seed:" + id, LibraryID: lib.ID, Type: model.TypeEpisode, ParentID: &season, SeasonAired: &sn, IndexAired: &en, RuntimeMs: &rt,
			Title: fmt.Sprintf("Episode %d: a title of ordinary length", en), Overview: "A synopsis of a couple of sentences, as providers write them, about what happens in this episode and who is in it."}))
		src := "seed-src-" + id
		must(st.ReplaceSource(ctx, &model.MediaSource{ID: src, ItemID: id, Container: "mkv", IsPrimary: true, Size: 400_000_000, DurationMs: rt, Bitrate: 2_000_000,
			Parts: []model.MediaPart{{ID: "seed-part-" + id, SourceID: src, Path: fmt.Sprintf("%s/Seed Show/Season %02d/Seed Show - S%02dE%04d [1080p][HEVC x265 10bit][Dual-Audio].mkv", *root, sn, sn, en)}},
			Streams: []model.MediaStream{
				{SourceID: src, Index: 0, Type: model.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080},
				{SourceID: src, Index: 1, Type: model.StreamAudio, Codec: "opus", Language: "jpn", Channels: 2},
				{SourceID: src, Index: 2, Type: model.StreamAudio, Codec: "opus", Language: "eng", Channels: 2},
				{SourceID: src, Index: 3, Type: model.StreamSubtitle, Codec: "ass", Language: "eng", Title: "Full"},
				{SourceID: src, Index: 4, Type: model.StreamSubtitle, Codec: "ass", Language: "eng", Title: "Signs & Songs"},
			}}, "{}"))
	}
	fmt.Printf("seeded %d episodes in %s\n", *n, *data)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
