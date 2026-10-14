package api

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/imaging"
	"github.com/roxy-media/roxy/server/internal/model"
)

// GET /v1/items/{id}/images/{type}[?w=<px>]: a cached poster/backdrop/logo/thumb as
// JPEG. ?w= resizes (never enlarges) and caches the variant as <type>.w<px>.jpg.
func (s *Server) getItemImage(w http.ResponseWriter, r *http.Request) {
	id, typ := chi.URLParam(r, "id"), chi.URLParam(r, "type")
	if !model.IsImageType(typ) {
		writeError(w, 404, "unknown image type")
		return
	}
	img, err := s.store.GetItemImage(r.Context(), id, typ)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if img == nil {
		writeError(w, 404, "no image")
		return
	}
	path := img.Path
	if width, _ := strconv.Atoi(r.URL.Query().Get("w")); width > 0 {
		if width < 16 {
			width = 16
		}
		if width > 4096 {
			width = 4096
		}
		if img.Width == 0 || width < img.Width {
			variant := filepath.Join(filepath.Dir(img.Path), fmt.Sprintf("%s.w%d.jpg", typ, width))
			if _, err := os.Stat(variant); err != nil {
				if err := resizeToFile(img.Path, variant, width); err != nil {
					writeError(w, 500, "resize failed: "+err.Error())
					return
				}
			}
			path = variant
		}
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "image file missing")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func resizeToFile(src, dst string, width int) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, imaging.Resize(img, width), &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// POST /v1/items/{id}/refresh: force a metadata lookup for the item (its series when
// it is a season or episode). 202.
func (s *Server) refreshItem(w http.ResponseWriter, r *http.Request) {
	it, err := s.store.GetItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if it == nil {
		writeError(w, 404, "item not found")
		return
	}
	if s.meta == nil {
		writeError(w, 503, "metadata service not running")
		return
	}
	s.meta.Refresh(it.ID)
	w.WriteHeader(http.StatusAccepted)
}
