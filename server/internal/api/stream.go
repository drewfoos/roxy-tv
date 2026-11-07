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
	"github.com/roxy-media/roxy/server/internal/safefs"
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
	// No part asked for is the first. One that is asked for must be a whole number: "abc"
	// (or "", "1.0") used to fail to parse and quietly serve part 0 instead, which looks
	// like the right file and is not (as with subtitle and attachment indexes).
	part := src.Parts[0]
	if q := r.URL.Query(); q.Has("part") {
		n, err := strconv.Atoi(q.Get("part"))
		if err != nil {
			writeError(w, 400, "invalid part")
			return
		}
		if n < 0 || n >= len(src.Parts) {
			writeError(w, 404, "no such part")
			return
		}
		part = src.Parts[n]
	}
	path, ok := mediaFile(w, part.Path)
	if !ok {
		return
	}
	f, err := os.Open(path)
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
	w.Header().Set("Content-Disposition", contentDisposition("inline", filepath.Base(part.Path)))
	http.ServeContent(w, r, filepath.Base(part.Path), fi.ModTime(), f)
}

// mediaFile resolves a media path (video, sidecar) and refuses one that a symlink leads
// out of the media folders: 403, and ok is false. A missing file is a 404.
func mediaFile(w http.ResponseWriter, path string) (string, bool) {
	real, err := safefs.MediaPath(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, 404, "file missing on disk")
		} else {
			writeError(w, 403, "file is outside the media folders")
		}
		return "", false
	}
	return real, true
}

// GET /v1/sources/{id}/attachments/{index}: an extracted font. The MKV names its own
// attachments' type, so anything that is not a font is served as an opaque download:
// an attachment tagged text/html or image/svg+xml must never run script on Roxy's origin.
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	idx, ok := pathIndex(w, r)
	if !ok {
		return
	}
	for _, a := range src.Attachments {
		if a.Index != idx {
			continue
		}
		if a.ExtractedPath == "" {
			// Extract lazily as a fallback; ingest should already have done this.
			if len(src.Parts) == 0 {
				writeError(w, 404, "source has no file")
				return
			}
			video, ok := mediaFile(w, src.Parts[0].Path)
			if !ok {
				return
			}
			dir := filepath.Join(s.cfg.AttachmentsDir(), src.ID)
			atts := src.Attachments
			if err := probe.ExtractAttachments(r.Context(), s.ff, video, dir, atts); err != nil {
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
		// Extracted fonts live in the attachment cache; nothing else is served from here.
		extracted, err := safefs.ResolveUnder(a.ExtractedPath, s.cfg.AttachmentsDir())
		if err == nil {
			err = safefs.Check(a.ExtractedPath)
		}
		if err != nil {
			writeError(w, 404, "attachment missing")
			return
		}
		f, err := os.Open(extracted)
		if err != nil {
			writeError(w, 404, "attachment missing")
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil || fi.IsDir() {
			writeError(w, 404, "attachment missing")
			return
		}
		setAttachmentHeaders(w.Header(), a)
		http.ServeContent(w, r, "", fi.ModTime(), f)
		return
	}
	writeError(w, 404, "attachment not found")
}

// setAttachmentHeaders: a font type from the allow-list (else octet-stream), no sniffing,
// always a download, and a CSP that forbids everything should a browser render it anyway.
func setAttachmentHeaders(h http.Header, a model.Attachment) {
	h.Set("Content-Type", attachmentMime(a.Mime, a.Filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition("attachment", safeAttachmentName(a.Filename)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
}

var fontMimes = map[string]bool{
	"application/x-truetype-font":   true,
	"application/x-font-ttf":        true,
	"application/x-font-otf":        true,
	"application/vnd.ms-opentype":   true,
	"application/font-sfnt":         true,
	"application/font-woff":         true,
	"application/x-font-truetype":   true,
	"application/x-font-opentype":   true,
	"application/vnd.ms-fontobject": true,
}

var fontExts = map[string]string{
	".ttf": "font/ttf", ".otf": "font/otf", ".ttc": "font/collection", ".otc": "font/collection",
	".woff": "font/woff", ".woff2": "font/woff2",
}

// attachmentMime returns the attachment's declared type only when it is a font;
// everything else (and anything malformed) is application/octet-stream.
func attachmentMime(declared, filename string) string {
	m := strings.ToLower(strings.TrimSpace(declared))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	if m != "" {
		for _, c := range m {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '/' || c == '-' || c == '.' || c == '+') {
				return "application/octet-stream"
			}
		}
		if strings.Count(m, "/") == 1 && (strings.HasPrefix(m, "font/") && len(m) > len("font/") || strings.HasPrefix(m, "application/x-font-") || fontMimes[m]) {
			return m
		}
		return "application/octet-stream"
	}
	// No declared type: trust only a font extension (never mime.TypeByExtension, which
	// would happily answer text/html for "x.html").
	if t, ok := fontExts[strings.ToLower(filepath.Ext(filename))]; ok {
		return t
	}
	return "application/octet-stream"
}

func safeAttachmentName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "attachment"
	}
	return name
}

// contentDisposition builds "<kind>; filename=..." with the name quoted and escaped (RFC
// 2231 filename*= for non-ASCII), so a quote or newline in a file name cannot break or
// add headers. Falls back to the bare kind if the name cannot be encoded.
func contentDisposition(kind, name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if v := mime.FormatMediaType(kind, map[string]string{"filename": name}); v != "" {
		return v
	}
	return kind
}

// GET /v1/sources/{id}/subtitles/{index}: a text subtitle track, byte for byte.
// Embedded tracks are extracted to the cache on first request; external sidecars are served directly.
func (s *Server) getSubtitle(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.GetSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		writeError(w, 404, "source not found")
		return
	}
	idx, ok := pathIndex(w, r)
	if !ok {
		return
	}
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
	var path string
	mime := st.Codec
	if probe.ConvertedToSRT(st.Codec) {
		mime = "subrip" // served converted (see probe.ConvertedToSRT)
	}
	sidecarCache := probe.SidecarCachePath(s.cfg.SubtitlesDir(), src.ID, *st)
	if st.ExternalPath != "" && sidecarCache == "" {
		// A sidecar next to the media: must really be inside a media folder (a symlink to
		// /config/roxy.db is refused).
		p, ok := mediaFile(w, st.ExternalPath)
		if !ok {
			return
		}
		path = p
	} else {
		ext := map[string]string{"ass": ".ass", "ssa": ".ass", "subrip": ".srt", "srt": ".srt", "webvtt": ".vtt", "vtt": ".vtt"}[st.Codec]
		if probe.ConvertedToSRT(st.Codec) {
			ext = ".srt"
		}
		path = filepath.Join(s.cfg.SubtitlesDir(), src.ID, fmt.Sprintf("%d%s", st.Index, ext))
		if sidecarCache != "" {
			// A track inside a .mks, or a SAMI file: extracted or converted into the cache
			// under the sidecar's time, so a replaced file is never served stale.
			path = sidecarCache
		}
		if _, err := os.Stat(path); err != nil {
			if len(src.Parts) == 0 {
				writeError(w, 404, "source has no file")
				return
			}
			input, index := "", st.Index
			if st.ExternalPath != "" {
				p, ok := mediaFile(w, st.ExternalPath)
				if !ok {
					return
				}
				input, index = p, 0 // the sidecar's own (only) stream
				if st.ExternalIndex != nil {
					index = *st.ExternalIndex // a track inside a .mks
				}
			} else {
				video, ok := mediaFile(w, src.Parts[0].Path)
				if !ok {
					return
				}
				input = video
			}
			if err := probe.EnsureTextSubtitle(r.Context(), s.ff, input, index, st.Codec, path); err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "subtitle file missing")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		writeError(w, 404, "subtitle file missing")
		return
	}
	w.Header().Set("Content-Type", subtitleMime(mime))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), f)
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

// pathIndex reads the {index} URL part: a track or attachment number, 0 or more. Anything
// else is a 400. It used to be parsed with the error ignored, so "abc" (or any typo) read
// as index 0 and could serve track 0 instead of being refused.
func pathIndex(w http.ResponseWriter, r *http.Request) (int, bool) {
	idx, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil || idx < 0 {
		writeError(w, 400, "invalid index")
		return 0, false
	}
	return idx, true
}
