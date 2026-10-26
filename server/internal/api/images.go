package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
			// Logos keep their transparency (PNG); photos are JPEG.
			ext := "jpg"
			if typ == "logo" {
				ext = "png"
			}
			variant := filepath.Join(filepath.Dir(img.Path), fmt.Sprintf("%s.w%d.%s", typ, width, ext))
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
	// The original keeps whatever the provider sent (sniffed); variants are named by format.
	ctype := "image/jpeg"
	if strings.HasSuffix(path, ".png") {
		ctype = "image/png"
	} else if path == img.Path {
		head := make([]byte, 512)
		if n, _ := f.Read(head); n > 0 {
			if t := http.DetectContentType(head[:n]); strings.HasPrefix(t, "image/") {
				ctype = t
			}
			_, _ = f.Seek(0, 0)
		}
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// resizeToFile writes a resized copy; PNG (alpha kept) when dst ends in .png, else JPEG.
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
	resized := imaging.Resize(img, width)
	if strings.HasSuffix(dst, ".png") {
		if err := png.Encode(&buf, resized); err != nil {
			return err
		}
	} else if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
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

// PUT /v1/items/{id}/images/{type} {url}: admin picks the art by hand (any https image,
// typically one of TMDB's alternatives). The choice is locked against refreshes. 204.
func (s *Server) putItemImage(w http.ResponseWriter, r *http.Request) {
	p := userFrom(w, r)
	if p == nil {
		return
	}
	if !p.IsAdmin {
		writeError(w, 403, "admin only")
		return
	}
	typ := chi.URLParam(r, "type")
	ok := false
	for _, t := range model.ImageTypes {
		ok = ok || t == typ
	}
	if !ok {
		writeError(w, 400, "unknown image type")
		return
	}
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
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil || !strings.HasPrefix(body.URL, "https://") {
		writeError(w, 400, "body must be {\"url\": \"https://...\"}")
		return
	}
	if err := s.meta.SetImage(r.Context(), it.ID, typ, body.URL); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/items/{id}/refresh: force a metadata lookup for the item (its series when
// it is a season or episode). 202.
func (s *Server) refreshItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
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
