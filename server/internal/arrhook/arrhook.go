// Package arrhook receives Sonarr / Radarr webhook notifications and keeps the library
// in step without a rescan: imports are ingested, renames re-ingested, deleted files and
// titles removed. See docs/api/arr-webhooks.md for the payloads.
//
// Paths in the payload are the arr app's view of the disk. In a Docker stack the mounts
// usually differ ("/data/media/tv" there, "/media/tv" here), so every path goes through
// the admin's mappings first and, failing that, a suffix match against the library
// roots (the tail of the path that exists under a root wins), which needs no config
// when the folder layout is the same on both sides.
package arrhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/store"
)

// PathMapping rewrites a prefix of the arr app's path into Roxy's.
type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// settings is the slice of the "arr" settings document this package reads.
type settings struct {
	PathMappings []PathMapping `json:"pathMappings"`
}

type Service struct {
	store *store.Store
	lib   *library.Library
}

func New(st *store.Store, lib *library.Library) *Service { return &Service{store: st, lib: lib} }

// ---- payloads (Go's decoder matches field names case-insensitively) ----

type file struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	PreviousPath string `json:"previousPath"`
}

type payload struct {
	EventType string `json:"eventType"`
	IsUpgrade bool   `json:"isUpgrade"`
	Series    *struct {
		Title string `json:"title"`
		Path  string `json:"path"`
	} `json:"series"`
	Movie *struct {
		Title      string `json:"title"`
		FolderPath string `json:"folderPath"`
	} `json:"movie"`
	Episodes []struct {
		SeasonNumber  int `json:"seasonNumber"`
		EpisodeNumber int `json:"episodeNumber"`
	} `json:"episodes"`
	EpisodeFile         *file  `json:"episodeFile"`
	EpisodeFiles        []file `json:"episodeFiles"`
	MovieFile           *file  `json:"movieFile"`
	DeletedFiles        []file `json:"deletedFiles"`
	RenamedEpisodeFiles []file `json:"renamedEpisodeFiles"`
	RenamedMovieFiles   []file `json:"renamedMovieFiles"`
	Message             string `json:"message"`
}

func (p *payload) title() string {
	switch {
	case p.Series != nil && len(p.Episodes) > 0:
		e := p.Episodes[0]
		return fmt.Sprintf("%s S%02dE%02d", p.Series.Title, e.SeasonNumber, e.EpisodeNumber)
	case p.Series != nil:
		return p.Series.Title
	case p.Movie != nil:
		return p.Movie.Title
	}
	return ""
}

func (p *payload) folder() string {
	if p.Series != nil {
		return p.Series.Path
	}
	if p.Movie != nil {
		return p.Movie.FolderPath
	}
	return ""
}

// absolute returns a file's path, building it from the folder and relative path when the
// payload only carries the latter.
func (p *payload) absolute(f file) string {
	if f.Path != "" {
		return f.Path
	}
	if f.RelativePath != "" && p.folder() != "" {
		return joinAny(p.folder(), f.RelativePath)
	}
	return ""
}

// joinAny joins with the separator the base already uses (payloads come from Linux containers).
func joinAny(base, rel string) string {
	sep := "/"
	if strings.Contains(base, `\`) && !strings.Contains(base, "/") {
		sep = `\`
	}
	return strings.TrimRight(base, `\/`) + sep + strings.TrimLeft(rel, `\/`)
}

// Handle processes one notification from source ("sonarr" | "radarr"). It never fails
// the HTTP call for a library problem: outcomes land in the events log.
func (s *Service) Handle(ctx context.Context, source string, body []byte) (store.WebhookEvent, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return store.WebhookEvent{}, fmt.Errorf("bad JSON: %w", err)
	}
	ev := store.WebhookEvent{Source: source, EventType: p.EventType, Title: p.title()}
	switch p.EventType {
	case "Download":
		// An upgrade replaces files: drop the old sources first, then ingest the new file.
		for _, f := range p.DeletedFiles {
			s.remove(ctx, p, f)
		}
		files := p.EpisodeFiles
		if p.EpisodeFile != nil {
			files = append(files, *p.EpisodeFile)
		}
		if p.MovieFile != nil {
			files = append(files, *p.MovieFile)
		}
		if len(files) == 0 {
			ev.Result = "ignored: no file in payload"
			break
		}
		var results []string
		for _, f := range files {
			local, ok := s.resolve(ctx, p.absolute(f))
			ev.Path = local
			if !ok {
				results = append(results, "error: path not under a library ("+p.absolute(f)+")")
				continue
			}
			if err := s.lib.IngestPath(ctx, local); err != nil {
				results = append(results, "error: "+err.Error())
			} else {
				results = append(results, "ingested")
			}
		}
		ev.Result = strings.Join(results, "; ")
	case "Rename":
		renamed := append(p.RenamedEpisodeFiles, p.RenamedMovieFiles...)
		n := 0
		for _, f := range renamed {
			if old, ok := s.resolveKnownFile(ctx, f.PreviousPath); ok {
				s.removePath(ctx, old)
			}
			local, ok := s.resolve(ctx, p.absolute(f))
			if ok && s.lib.IngestPath(ctx, local) == nil {
				n++
			}
			ev.Path = local
		}
		s.prune(ctx)
		ev.Result = fmt.Sprintf("renamed %d of %d", n, len(renamed))
	case "EpisodeFileDelete", "MovieFileDelete":
		var f file
		if p.EpisodeFile != nil {
			f = *p.EpisodeFile
		} else if p.MovieFile != nil {
			f = *p.MovieFile
		}
		local, ok := s.resolveKnownFile(ctx, p.absolute(f))
		ev.Path = local
		if !ok {
			ev.Result = "ignored: not in library"
			break
		}
		if s.removePath(ctx, local) {
			s.prune(ctx)
			ev.Result = "removed"
		} else {
			ev.Result = "ignored: not in library"
		}
	case "SeriesDelete", "MovieDelete":
		local, ok := s.resolveKnownFolder(ctx, p.folder())
		ev.Path = local
		if !ok {
			ev.Result = "ignored: unknown folder"
			break
		}
		n, err := s.store.DeleteSourcesUnder(ctx, local)
		if err != nil {
			ev.Result = "error: " + err.Error()
			break
		}
		s.prune(ctx)
		ev.Result = fmt.Sprintf("removed %d file(s)", n)
	case "Test":
		ev.Result = "ok"
	case "Grab", "SeriesAdd", "MovieAdded", "Health", "HealthRestored", "ApplicationUpdate", "ManualInteractionRequired":
		ev.Result = "logged"
	default:
		ev.Result = "ignored: unknown event"
	}
	if err := s.store.AddWebhookEvent(ctx, ev); err != nil {
		slog.Warn("webhook event not recorded", "err", err)
	}
	slog.Info("webhook", "source", source, "event", p.EventType, "title", ev.Title, "result", ev.Result)
	return ev, nil
}

func (s *Service) remove(ctx context.Context, p payload, f file) {
	if local, ok := s.resolveKnownFile(ctx, p.absolute(f)); ok {
		s.removePath(ctx, local)
	}
}

// resolveKnownFile is resolve() for a file that may no longer exist on disk: the suffix
// match asks the library instead of the filesystem.
func (s *Service) resolveKnownFile(ctx context.Context, p string) (string, bool) {
	cfg, roots, ok := s.mappingsAndRoots(ctx)
	if !ok {
		return "", false
	}
	return Resolve(p, cfg.PathMappings, roots, func(path string) bool {
		part, err := s.store.GetPartByPath(ctx, path)
		return err == nil && part != nil
	})
}

// resolveKnownFolder is the same for a series or movie folder.
func (s *Service) resolveKnownFolder(ctx context.Context, p string) (string, bool) {
	cfg, roots, ok := s.mappingsAndRoots(ctx)
	if !ok {
		return "", false
	}
	return Resolve(p, cfg.PathMappings, roots, func(path string) bool {
		n, err := s.store.CountSourcesUnder(ctx, path)
		return err == nil && n > 0
	})
}

func (s *Service) mappingsAndRoots(ctx context.Context) (settings, []string, bool) {
	var cfg settings
	_, _ = s.store.GetSetting(ctx, "arr", &cfg)
	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return cfg, nil, false
	}
	var roots []string
	for _, l := range libs {
		roots = append(roots, l.Root)
	}
	return cfg, roots, true
}

// removePath drops the source whose first part is at path. True when one was there.
func (s *Service) removePath(ctx context.Context, path string) bool {
	part, err := s.store.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		return false
	}
	return s.store.DeleteSource(ctx, part.SourceID) == nil
}

func (s *Service) prune(ctx context.Context) {
	if n, err := s.store.PruneOrphans(ctx); err == nil && n > 0 {
		slog.Info("webhook: pruned items without files", "n", n)
	}
}

// resolve turns an arr-side path into a local one under a library root, or ok=false.
func (s *Service) resolve(ctx context.Context, p string) (string, bool) {
	if p == "" {
		return "", false
	}
	cfg, roots, ok := s.mappingsAndRoots(ctx)
	if !ok {
		return "", false
	}
	return Resolve(p, cfg.PathMappings, roots, func(path string) bool { _, err := os.Stat(path); return err == nil })
}

// Resolve applies the mappings (longest prefix first), then checks the result is under a
// root; otherwise it tries every suffix of the path under every root and keeps the first
// that exists. Pure, so it is unit-tested with a fake filesystem.
func Resolve(p string, mappings []PathMapping, roots []string, exists func(string) bool) (string, bool) {
	norm := func(s string) string { return strings.ReplaceAll(s, `\`, "/") }
	src := norm(p)
	best := ""
	for _, m := range mappings {
		from := strings.TrimRight(norm(m.From), "/")
		if from != "" && (src == from || strings.HasPrefix(src, from+"/")) && len(from) > len(best) {
			best = from
			src = strings.TrimRight(norm(m.To), "/") + strings.TrimPrefix(src, from)
		}
	}
	under := func(path string) (string, bool) {
		for _, r := range roots {
			root := strings.TrimRight(norm(r), "/")
			if path == root || strings.HasPrefix(path, root+"/") {
				return filepath.FromSlash(path), true
			}
		}
		return "", false
	}
	if local, ok := under(src); ok && exists(local) {
		return local, true
	}
	// Suffix match: "/data/media/tv/Show/S01/x.mkv" against root "D:/media/tv" tries
	// "Show/S01/x.mkv", "S01/x.mkv", "x.mkv" and keeps the longest that exists.
	parts := strings.Split(strings.Trim(src, "/"), "/")
	for i := 0; i < len(parts); i++ {
		tail := strings.Join(parts[i:], "/")
		for _, r := range roots {
			candidate := filepath.FromSlash(strings.TrimRight(norm(r), "/") + "/" + tail)
			if exists(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}
