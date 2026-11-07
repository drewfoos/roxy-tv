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
	"path"
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

// episodeHint: the season/episode(s) in the payload, for a single-file import.
func (p *payload) episodeHint() *library.EpisodeHint {
	pairs := make([][2]int, 0, len(p.Episodes))
	for _, e := range p.Episodes {
		pairs = append(pairs, [2]int{e.SeasonNumber, e.EpisodeNumber})
	}
	return library.NewEpisodeHint(pairs)
}

func (p *payload) title() string {
	switch {
	case p.Series != nil && len(p.Episodes) > 0:
		e := p.Episodes[0]
		return fmt.Sprintf("%s S%d E%d", p.Series.Title, e.SeasonNumber, e.EpisodeNumber)
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
		// Sonarr's episode list describes the file only when there is exactly one.
		var hint *library.EpisodeHint
		if len(files) == 1 && p.EpisodeFile != nil {
			hint = p.episodeHint()
		}
		var results []string
		for _, f := range files {
			local, ok := s.resolve(ctx, p.absolute(f))
			ev.Path = local
			if !ok {
				results = append(results, "error: path not under a library ("+p.absolute(f)+")")
				continue
			}
			if err := s.lib.IngestPathHinted(ctx, local, hint); err != nil {
				results = append(results, "error: "+err.Error())
			} else {
				results = append(results, "ingested")
			}
		}
		ev.Result = strings.Join(results, "; ")
	case "Rename":
		renamed := append(p.RenamedEpisodeFiles, p.RenamedMovieFiles...)
		n := 0
		var moves []library.Move
		for _, f := range renamed {
			// Keep the episode the old name had: the new name may be one the parser
			// cannot read, and the payload carries no episode list for renames.
			var hint *library.EpisodeHint
			var mv library.Move
			if old, ok := s.resolveKnownFile(ctx, f.PreviousPath); ok {
				hint = s.lib.HintForPath(ctx, old)
				// The item it had, so its watch history can follow it if the new path
				// makes it a new item (a renamed series or movie folder).
				mv.OldPath = old
				if it, _ := s.store.ItemForPath(ctx, old); it != nil {
					mv.OldChain, _ = s.store.ItemChain(ctx, it.ID)
				}
				s.removePath(ctx, old)
			}
			local, ok := s.resolve(ctx, p.absolute(f))
			if ok && s.lib.IngestPathHinted(ctx, local, hint) == nil {
				n++
				if len(mv.OldChain) > 0 {
					mv.NewPath = local
					moves = append(moves, mv)
				}
			}
			ev.Path = local
		}
		s.lib.CarryMoves(ctx, moves) // before the old items are pruned
		s.lib.SettleDepartures(ctx)  // a rename whose new path could not be ingested now
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
			s.lib.SettleDepartures(ctx)
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
		// Noted first: the same show or film added again elsewhere gets its history back.
		if err := s.store.RecordDeparturesUnder(ctx, local); err != nil {
			slog.Warn("webhook: noting removed files", "folder", local, "err", err)
		}
		n, err := s.store.DeleteSourcesUnder(ctx, local)
		if err != nil {
			ev.Result = "error: " + err.Error()
			break
		}
		s.lib.SettleDepartures(ctx)
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
	local, found := Resolve(p, cfg.PathMappings, roots, func(path string) bool {
		n, err := s.store.CountSourcesUnder(ctx, path)
		return err == nil && n > 0
	})
	// A series or movie folder sits inside a library root. A root itself (or anything
	// above one) would drop a whole library from the index on a "delete" webhook.
	if found && rootOrAbove(local, roots) {
		return "", false
	}
	return local, found
}

// rootOrAbove: path is one of the roots or contains one.
func rootOrAbove(p string, roots []string) bool {
	clean := filepath.Clean(p)
	for _, r := range roots {
		r = filepath.Clean(r)
		if clean == r || strings.HasPrefix(r, clean+string(filepath.Separator)) || clean == string(filepath.Separator) {
			return true
		}
	}
	return false
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

// IngestArrFiles adds files an arr app reports having (its own paths) that the library
// does not have yet. The download tracker calls it when a request's file count goes up,
// so an import shows up in seconds even without a webhook set up. Returns how many were added.
func (s *Service) IngestArrFiles(ctx context.Context, paths []string) int {
	n := 0
	for _, p := range paths {
		local, ok := s.resolve(ctx, p)
		if !ok {
			continue
		}
		if part, err := s.store.GetPartByPath(ctx, local); err == nil && part != nil {
			continue // already in the library
		}
		if err := s.lib.IngestPath(ctx, local); err != nil {
			slog.Warn("import: ingest", "path", local, "err", err)
			continue
		}
		n++
	}
	return n
}

// removePath drops the source whose first part is at path, noting its departure first so
// its watch history can follow it if it turns up again (moved, renamed, fetched again).
// True when one was there.
func (s *Service) removePath(ctx context.Context, path string) bool {
	part, err := s.store.GetPartByPath(ctx, path)
	if err != nil || part == nil {
		return false
	}
	if err := s.store.RecordDeparture(ctx, path); err != nil {
		slog.Warn("webhook: noting a removed file", "path", path, "err", err)
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

// Resolve applies the longest matching mapping (one only), then checks the result is under a
// root; otherwise it tries every suffix of the path under every root and keeps the longest
// that exists. A suffix found under several roots is taken only when the arr path names
// one of them; otherwise nothing is resolved. Pure, so it is unit-tested with a fake
// filesystem.
func Resolve(p string, mappings []PathMapping, roots []string, exists func(string) bool) (string, bool) {
	norm := func(s string) string { return strings.ReplaceAll(s, `\`, "/") }
	// Cleaned before any check: "/root/../x" must not pass as under "/root".
	src := path.Clean("/" + strings.TrimLeft(norm(p), "/"))
	if strings.Contains(norm(p), ":") && !strings.HasPrefix(norm(p), "/") {
		src = path.Clean(norm(p)) // a Windows drive path ("D:/media/...")
	}
	// One mapping at most: the longest From that is the path or a folder above it, compared
	// against the path as the arr app sent it. Rewriting as the loop went made the result
	// depend on list order, and let a later mapping match the already rewritten path.
	best, bestTo := "", ""
	for _, m := range mappings {
		from := strings.TrimRight(norm(m.From), "/")
		if from != "" && (src == from || strings.HasPrefix(src, from+"/")) && len(from) > len(best) {
			best, bestTo = from, strings.TrimRight(norm(m.To), "/")
		}
	}
	if best != "" {
		src = bestTo + strings.TrimPrefix(src, best)
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
		if tail == "" || tail == "." || strings.Contains("/"+tail+"/", "/../") {
			continue // "/" would map onto a root itself
		}
		var hits []string // candidates, one per root where the tail exists
		var bases []string
		for _, r := range roots {
			root := strings.TrimRight(norm(r), "/")
			candidate := filepath.FromSlash(root + "/" + tail)
			if exists(candidate) {
				hits = append(hits, candidate)
				bases = append(bases, path.Base(root))
			}
		}
		if len(hits) == 1 {
			return hits[0], true
		}
		if len(hits) > 1 {
			// The same folder in two libraries ("Avatar" in TV and in Anime). The folder
			// above the tail in the arr path usually names the library ("/data/anime/
			// Avatar" against root ".../Anime"): take the one root it names, else refuse.
			// Taking the first root deleted the wrong library's files on a folder delete.
			if i == 0 {
				return "", false
			}
			if c, ok := onlyMatch(hits, bases, parts[i-1], func(a, b string) bool { return a == b }); ok {
				return c, true
			}
			if c, ok := onlyMatch(hits, bases, parts[i-1], strings.EqualFold); ok {
				return c, true
			}
			return "", false
		}
	}
	return "", false
}

// onlyMatch returns the hit whose root base name equals parent, when exactly one does.
func onlyMatch(hits, bases []string, parent string, eq func(a, b string) bool) (string, bool) {
	found := ""
	n := 0
	for j, b := range bases {
		if eq(b, parent) {
			found = hits[j]
			n++
		}
	}
	return found, n == 1
}
