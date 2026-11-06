package api

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
)

// The first requests for a resized variant often arrive together. They used to share
// one dst+".tmp", so they truncated and renamed away each other's file and most failed.
// Every request must get the image and no temp file may be left behind.
func TestImageVariantConcurrentFirstRequests(t *testing.T) {
	for round := 0; round < 5; round++ {
		data := t.TempDir()
		safefs.Init(data)
		dir := filepath.Join(data, "cache", "images", "item1")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1600, 900)), nil); err != nil {
			t.Fatal(err)
		}
		orig := filepath.Join(dir, "backdrop.jpg")
		if err := os.WriteFile(orig, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		img := &store.ItemImage{ItemID: "item1", Type: "backdrop", Path: orig, Width: 1600, Height: 900}
		s := &Server{}
		const n = 16
		codes := make([]int, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				rec := httptest.NewRecorder()
				s.writeImage(rec, httptest.NewRequest(http.MethodGet, "/x?w=300", nil), img, "backdrop")
				codes[i] = rec.Code
				if rec.Code == 200 {
					if _, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
						codes[i] = -1
					}
				}
			}(i)
		}
		close(start)
		wg.Wait()
		for i, c := range codes {
			if c != 200 {
				t.Fatalf("round %d request %d: status %d", round, i, c)
			}
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Fatalf("temp file left behind: %s", e.Name())
			}
		}
	}
}
