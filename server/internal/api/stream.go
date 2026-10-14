package api

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
)

// GET /stream/{id} and /v1/stream/{id}: direct play with HTTP range support.
// {id} is a media source id, or (convenience) a file name stem such as "test-hi10p-ass".
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	src, err := s.store.GetSource(r.Context(), id)
	if err == nil && src == nil {
		src, err = s.store.FindSourceByPathStem(r.Context(), strings.TrimSuffix(id, filepath.Ext(id)))
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if src == nil || len(src.Parts) == 0 {
		writeError(w, 404, "source not found")
		return
	}
	part := src.Parts[0]
	if n, err := strconv.Atoi(r.URL.Query().Get("part")); err == nil && n < len(src.Parts) {
		part = src.Parts[n]
	}
	f, err := os.Open(part.Path)
	if err != nil {
		writeError(w, 404, "file missing on disk")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", containerMime(src.Container))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, fi.ModTime().UnixNano(), fi.Size()))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(part.Path)+"\"")
	http.ServeContent(w, r, filepath.Base(part.Path), fi.ModTime(), f)
}

// GET /v1/sources/{id}/attachments/{index}: an extracted font (or any attachment).
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "index"))
	for _, a := range src.Attachments {
		if a.Index != idx {
			continue
		}
		if a.ExtractedPath == "" {
			// Extract lazily as a fallback; ingest should already have done this.
			dir := filepath.Join(s.cfg.AttachmentsDir(), src.ID)
			atts := src.Attachments
			if err := probe.ExtractAttachments(r.Context(), s.ff, src.Parts[0].Path, dir, atts); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			for _, b := range atts {
				if b.Index == idx {
					a = b
				}
			}
			if a.ExtractedPath == "" {
				writeError(w, 404, "attachment could not be extracted")
				return
			}
		}
		ct := a.Mime
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(a.Filename))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, a.ExtractedPath)
		return
	}
	writeError(w, 404, "attachment not found")
}

// GET /v1/sources/{id}/subtitles/{index}: a text subtitle track, byte for byte.
// Embedded tracks are extracted to the cache on first request; external sidecars are served directly.
func (s *Server) getSubtitle(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "index"))
	var st *model.MediaStream
	for i := range src.Streams {
		if src.Streams[i].Index == idx && src.Streams[i].Type == model.StreamSubtitle {
			st = &src.Streams[i]
			break
		}
	}
	if st == nil {
		writeError(w, 404, "subtitle stream not found")
		return
	}
	if !st.IsTextFormat {
		writeError(w, 415, "image subtitles are not served as files yet")
		return
	}
	path := st.ExternalPath
	if path == "" {
		ext := map[string]string{"ass": ".ass", "ssa": ".ass", "subrip": ".srt", "srt": ".srt", "webvtt": ".vtt", "vtt": ".vtt"}[st.Codec]
		path = filepath.Join(s.cfg.SubtitlesDir(), src.ID, fmt.Sprintf("%d%s", st.Index, ext))
		if _, err := os.Stat(path); err != nil {
			if err := probe.ExtractTextSubtitle(r.Context(), s.ff, src.Parts[0].Path, st.Index, st.Codec, path); err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
	}
	w.Header().Set("Content-Type", subtitleMime(st.Codec))
	http.ServeFile(w, r, path)
}

func containerMime(c string) string {
	switch c {
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "mp4", "m4v":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "avi":
		return "video/x-msvideo"
	case "ts":
		return "video/mp2t"
	}
	return "application/octet-stream"
}

func subtitleMime(codec string) string {
	switch codec {
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	case "subrip", "srt":
		return "application/x-subrip; charset=utf-8"
	case "webvtt", "vtt":
		return "text/vtt; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}
