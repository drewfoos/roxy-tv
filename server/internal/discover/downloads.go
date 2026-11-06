package discover

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/roxy-media/roxy/server/internal/arr"
	"github.com/roxy-media/roxy/server/internal/store"
)

// Download tracking: what happened to a request after Sonarr / Radarr took it.
//
// A progress bar alone never says when a download stalls, fails, cannot be imported,
// or when nothing was found. Here every sent request gets a status built from
// the title's own queue (unpaged), its episodes (files, air dates, last search) and its
// history (failed downloads), refreshed every 10 s while something is moving and every
// 2 min otherwise, and at once when a Sonarr / Radarr webhook arrives.

// Download states, worst first in importance order (see rank).
const (
	DLFailed      = "failed"      // the download failed (the arr's reason in Message)
	DLAttention   = "attention"   // downloaded but the arr cannot import it
	DLStalled     = "stalled"     // a warning from the arr, paused, or no progress for a while
	DLDownloading = "downloading" // progress and time left
	DLImporting   = "importing"   // finished, the arr is moving it into the library
	DLQueued      = "queued"      // waiting in the download client (or a delay profile)
	DLNotFound    = "notFound"    // searched, nothing grabbed (the arr keeps looking)
	DLSearching   = "searching"   // sent, no search result yet
	DLWaiting     = "waiting"     // not released / aired yet
	DLComplete    = "complete"    // every requested, aired episode (or the movie) has a file
)

func rank(state string) int {
	switch state {
	case DLFailed:
		return 9
	case DLAttention:
		return 8
	case DLStalled:
		return 7
	case DLDownloading:
		return 6
	case DLImporting:
		return 5
	case DLQueued:
		return 4
	case DLNotFound:
		return 3
	case DLSearching:
		return 2
	case DLWaiting:
		return 1
	}
	return 0
}

// active: worth polling often.
func active(state string) bool {
	switch state {
	case DLDownloading, DLImporting, DLQueued, DLSearching, DLStalled:
		return true
	}
	return false
}

// Download is a request's download status.
type Download struct {
	State       string         `json:"state"`
	Progress    float64        `json:"progress"`              // 0..1 over what is downloading now
	TimeLeftSec int            `json:"timeLeftSec,omitempty"` // whole request; 0 = unknown
	Message     string         `json:"message,omitempty"`     // the arr's own words for a problem
	Have        int            `json:"have"`                  // requested, aired episodes with a file (movie: 0/1)
	Wanted      int            `json:"wanted"`                // requested, aired episodes (movie: 1 once released)
	Upcoming    int            `json:"upcoming,omitempty"`    // requested episodes not aired yet
	NextAir     *time.Time     `json:"nextAir,omitempty"`     // next requested episode / the movie's release
	LastSearch  *time.Time     `json:"lastSearch,omitempty"`
	Items       []DownloadItem `json:"items,omitempty"` // one per download (a season pack is one)
	// Episodes: every requested episode still missing, with its own state (series only).
	Episodes []EpisodeDL `json:"episodes,omitempty"`
	// Groups is Items folded by state for display: "Downloading · Season 1 · 10 episodes".
	Groups []DownloadGroup `json:"groups,omitempty"`
	// CanSearch: something is missing, not downloading, not being searched for right now
	// and not searched in the last few minutes. CanRetry: a download failed, cannot be
	// imported, or has been stalled long enough that replacing it is worth it.
	CanSearch bool `json:"canSearch"`
	CanRetry  bool `json:"canRetry"`
	// WaitingFor says what a "waiting" request waits for: "airing" (episodes not aired),
	// "release" (a movie not out at all) or "digital" (a movie in cinemas; Radarr waits
	// for the digital / physical release).
	WaitingFor string `json:"waitingFor,omitempty"`
	// IsProblem: failed, cannot be imported, or stalled (the admin Requests filter).
	IsProblem bool      `json:"isProblem"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// The rules behind CanSearch / CanRetry (and what the actions enforce).
const (
	searchCooldown  = 10 * time.Minute // a search returns within seconds; again right away finds the same
	stallRetryAfter = 10 * time.Minute // a stalled download often recovers; give it time first
)

// EpisodeDL is one requested episode that has no file yet.
type EpisodeDL struct {
	Season      int     `json:"season"`
	Episode     int     `json:"episode"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress,omitempty"`
	TimeLeftSec int     `json:"timeLeftSec,omitempty"`
	Message     string  `json:"message,omitempty"`
	AirDate     string  `json:"airDate,omitempty"` // waiting: when it airs (RFC 3339)
	CanSearch   bool    `json:"canSearch"`
	CanRetry    bool    `json:"canRetry"`
}

// DownloadGroup is every item in one state, with the episodes they cover.
type DownloadGroup struct {
	State       string  `json:"state"`
	Label       string  `json:"label"`
	Count       int     `json:"count"` // downloads (or missing episodes) in this state
	Progress    float64 `json:"progress"`
	TimeLeftSec int     `json:"timeLeftSec,omitempty"`
	Message     string  `json:"message,omitempty"`
}

// DownloadItem is one download in the client (or one failed / missing episode).
type DownloadItem struct {
	Label       string  `json:"label"` // "S1 E3", "Season 1 · 12 episodes", the movie title
	Release     string  `json:"release,omitempty"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	TimeLeftSec int     `json:"timeLeftSec,omitempty"`
	Message     string  `json:"message,omitempty"`
	CanRetry    bool    `json:"canRetry"`
	queueIDs    []int
	episodeIDs  []int
	eps         []arr.Episode
}

type tracker struct {
	mu       sync.Mutex
	status   map[string]*Download // request id -> status
	progress map[string]progressMark
	kick     chan struct{}
	refresh  map[string]time.Time // arr name -> last RefreshMonitoredDownloads
	stalled  map[string]time.Time // download id -> first seen stalled
}

type progressMark struct {
	left  float64
	since time.Time
}

const stallAfter = 10 * time.Minute

// stallQuiet: how long the client's stall warning is shown as "looking for peers" rather
// than "stalled". Every new torrent reports "stalled, no connections" for its first
// moments; showing that as a problem 6 s after a grab alarmed for nothing.
const stallQuiet = 2 * time.Minute

// Download returns a request's last known status (nil before the first look).
func (s *Service) Download(requestID string) *Download {
	s.dl.mu.Lock()
	defer s.dl.mu.Unlock()
	if d := s.dl.status[requestID]; d != nil {
		c := *d
		return &c
	}
	return nil
}

// KickDownloads makes the tracker look now (a webhook arrived, a request was sent).
func (s *Service) KickDownloads() {
	select {
	case s.dl.kick <- struct{}{}:
	default:
	}
}

func (s *Service) startTracker(ctx context.Context) {
	go func() {
		for {
			wait := s.trackOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-s.dl.kick:
				// a short pause lets the arr app finish what the webhook announced
				time.Sleep(2 * time.Second)
			case <-time.After(wait):
			}
		}
	}()
}

// trackOnce refreshes every sent request and says how long to wait before the next pass.
func (s *Service) trackOnce(ctx context.Context) time.Duration {
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return time.Minute
	}
	sonarr, radarr := s.clients()
	anyActive := false
	seen := map[string]bool{}
	for i := range all {
		r := &all[i]
		if r.Status != "sent" || r.ArrID == 0 {
			continue
		}
		c := sonarr
		if r.MediaType == "movie" {
			c = radarr
		}
		if c == nil {
			continue
		}
		seen[r.ID] = true
		prev := s.Download(r.ID)
		// Finished titles are re-checked rarely (a deleted file, a new season airing).
		// (One finishing an upgrade carries a message and keeps being refreshed.)
		if prev != nil && prev.State == DLComplete && prev.Upcoming == 0 && prev.Message == "" && time.Since(prev.UpdatedAt) < 10*time.Minute {
			continue
		}
		s.refreshClient(ctx, c, prev)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		d, err := s.trackRequest(cctx, c, r)
		cancel()
		if err != nil {
			slog.Debug("downloads: track", "title", r.Title, "err", err)
			continue
		}
		if prev == nil || prev.State != d.State {
			slog.Info("downloads: status", "title", r.Title, "state", d.State, "have", d.Have, "wanted", d.Wanted, "msg", d.Message)
		}
		d.IsProblem = d.State == DLFailed || d.State == DLAttention || d.State == DLStalled
		// The arr app has more files than last time: an import finished. Add them to the
		// library now rather than waiting for a webhook or a scan. (After a restart there
		// is no "last time"; the startup scan covers that.)
		if prev != nil && d.Have > prev.Have && s.importHook != nil {
			s.ingestImported(ctx, c, r)
		}
		s.dl.mu.Lock()
		s.dl.status[r.ID] = d
		s.dl.mu.Unlock()
		if active(d.State) {
			anyActive = true
		}
	}
	s.dl.mu.Lock()
	for id := range s.dl.status {
		if !seen[id] {
			delete(s.dl.status, id)
		}
	}
	s.dl.mu.Unlock()
	if anyActive {
		return 10 * time.Second
	}
	return 2 * time.Minute
}

// SetImportHook: how the tracker adds newly imported files to the library (the arr
// app's paths in, how many were added out). Set before Start.
func (s *Service) SetImportHook(f func(ctx context.Context, arrPaths []string) int) { s.importHook = f }

// ingestImported hands the title's files, as the arr app names them, to the import hook.
func (s *Service) ingestImported(ctx context.Context, c *arr.Client, r *store.Request) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var paths []string
	if c.Name == "radarr" {
		m, err := c.Movie(cctx, r.ArrID)
		if err != nil || m.MovieFile == nil {
			return
		}
		paths = []string{m.MovieFile.Path}
	} else {
		p, err := c.EpisodeFilePaths(cctx, r.ArrID)
		if err != nil {
			slog.Debug("downloads: episode files", "title", r.Title, "err", err)
			return
		}
		paths = p
	}
	if n := s.importHook(cctx, paths); n > 0 {
		slog.Info("downloads: added to the library", "title", r.Title, "files", n)
	}
}

// refreshClient asks the arr app to poll its download client (at most every 30 s, and
// only while something is in flight), so progress is not a minute stale.
func (s *Service) refreshClient(ctx context.Context, c *arr.Client, prev *Download) {
	if prev != nil && !active(prev.State) {
		return
	}
	s.dl.mu.Lock()
	last := s.dl.refresh[c.Name]
	if time.Since(last) < 30*time.Second {
		s.dl.mu.Unlock()
		return
	}
	s.dl.refresh[c.Name] = time.Now()
	s.dl.mu.Unlock()
	_ = c.RefreshDownloads(ctx)
}

func (s *Service) trackRequest(ctx context.Context, c *arr.Client, r *store.Request) (*Download, error) {
	queue, err := c.Queue(ctx, r.ArrID)
	if err != nil {
		return nil, err
	}
	history, _ := c.History(ctx, r.ArrID) // optional: failures only
	cmds, _ := c.ActiveSearches(ctx)      // optional: "searching right now"
	now := time.Now()
	if c.Name == "radarr" {
		m, err := c.Movie(ctx, r.ArrID)
		if err != nil {
			return nil, err
		}
		return s.movieStatusWith(r, m, queue, history, cmds, now), nil
	}
	eps, err := c.Episodes(ctx, r.ArrID)
	if err != nil {
		return nil, err
	}
	return s.seriesStatusWith(r, eps, queue, history, cmds, now), nil
}

// beingSearched: episodes (by id) an active search of the arr app covers.
func beingSearched(cmds []arr.Command, seriesID int, eps map[int]arr.Episode) map[int]bool {
	out := map[int]bool{}
	for _, cmd := range cmds {
		for id, e := range eps {
			if cmd.Covers(seriesID, e, 0) {
				out[id] = true
			}
		}
	}
	return out
}

// cooledDown: not searched within searchCooldown.
func cooledDown(last *time.Time, now time.Time) bool {
	return last == nil || now.Sub(*last) >= searchCooldown
}

// inScope: whether a Sonarr episode is part of what this request asked for.
func inScope(r *store.Request, e arr.Episode) bool {
	if containsStr(r.Episodes, fmt.Sprintf("S%dE%d", e.SeasonNumber, e.EpisodeNumber)) {
		return true
	}
	var seasons []int
	for _, n := range r.Seasons {
		if n >= 0 {
			seasons = append(seasons, n)
		}
	}
	if len(seasons) == 0 {
		if len(r.Seasons) > 0 { // "episodes only" sentinel: nothing beyond the episodes
			return false
		}
		return e.SeasonNumber > 0 && e.Monitored // whole series: what Sonarr was told to watch
	}
	return contains(seasons, e.SeasonNumber)
}

func (s *Service) seriesStatus(r *store.Request, eps []arr.Episode, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
	return s.seriesStatusWith(r, eps, queue, history, nil, now)
}

func (s *Service) seriesStatusWith(r *store.Request, eps []arr.Episode, queue []arr.QueueItem, history []arr.HistoryRecord, cmds []arr.Command, now time.Time) *Download {
	d := &Download{UpdatedAt: now}
	byID := map[int]arr.Episode{}
	var missing []arr.Episode
	for _, e := range eps {
		if !inScope(r, e) {
			continue
		}
		byID[e.ID] = e
		if !e.Aired(now) {
			if !e.HasFile {
				d.Upcoming++
				if e.AirDateUTC != nil && (d.NextAir == nil || e.AirDateUTC.Before(*d.NextAir)) {
					t := *e.AirDateUTC
					d.NextAir = &t
				}
			}
			continue
		}
		d.Wanted++
		if e.HasFile {
			d.Have++
		} else {
			missing = append(missing, e)
		}
		if e.LastSearchTime != nil && (d.LastSearch == nil || e.LastSearchTime.After(*d.LastSearch)) {
			t := *e.LastSearchTime
			d.LastSearch = &t
		}
	}
	// Queue: one item per download (a pack covers several episodes).
	inQueue := map[int]bool{}
	groups := map[string][]arr.QueueItem{}
	var order []string
	for _, q := range queue {
		if q.EpisodeID != 0 {
			if _, ok := byID[q.EpisodeID]; !ok && len(byID) > 0 {
				continue // a download for an episode outside this request
			}
			inQueue[q.EpisodeID] = true
		}
		key := q.DownloadID
		if key == "" {
			key = fmt.Sprintf("q%d", q.ID)
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], q)
	}
	for _, key := range order {
		d.Items = append(d.Items, s.queueItem(key, groups[key], byID, now))
	}
	// Episodes not downloading: failed last time, or searched without a grab.
	last := latestEvents(history)
	var failed, notFound, grabbed []arr.Episode
	failMsg := ""
	for _, e := range missing {
		if inQueue[e.ID] {
			continue
		}
		if ev, ok := last[e.ID]; ok && ev.EventType == "downloadFailed" {
			failed = append(failed, e)
			if failMsg == "" {
				failMsg = ev.Data["message"]
			}
			continue
		}
		if ev, ok := last[e.ID]; ok && justGrabbed(ev, now) {
			grabbed = append(grabbed, e)
			continue
		}
		if e.LastSearchTime != nil {
			if ev, ok := last[e.ID]; !ok || ev.Date.Before(*e.LastSearchTime) {
				notFound = append(notFound, e)
			}
		}
	}
	if len(failed) > 0 {
		it := DownloadItem{Label: episodesLabel(failed), State: DLFailed, Message: failMsg, eps: failed, CanRetry: true}
		for _, e := range failed {
			it.episodeIDs = append(it.episodeIDs, e.ID)
		}
		d.Items = append(d.Items, it)
	}
	if len(grabbed) > 0 {
		it := DownloadItem{Label: episodesLabel(grabbed), State: DLQueued, Message: sentToClient, eps: grabbed}
		for _, e := range grabbed {
			it.episodeIDs = append(it.episodeIDs, e.ID)
		}
		d.Items = append(d.Items, it)
	}
	if len(notFound) > 0 && len(d.Items) > 0 {
		it := DownloadItem{Label: episodesLabel(notFound), State: DLNotFound, eps: notFound}
		for _, e := range notFound {
			it.episodeIDs = append(it.episodeIDs, e.ID)
		}
		d.Items = append(d.Items, it)
	}
	// Everything asked for is here and the only downloads replace files that exist (an
	// upgrade, a dub for a sub-only file): the title is complete, not "downloading".
	upgrading := d.Wanted > 0 && d.Have >= d.Wanted && len(d.Items) > 0
	for _, it := range d.Items {
		if len(it.queueIDs) == 0 || len(it.eps) == 0 || !allHaveFiles(it.eps, byID) {
			upgrading = false
		}
	}
	if upgrading {
		up := d.Items
		d.Items = nil
		s.summarise(d, 0, 0)
		d.Message = upgradeMessage(up)
		d.Episodes = nil
		return d
	}
	s.summarise(d, len(missing), len(notFound))
	if len(byID) == 0 && len(d.Items) == 0 {
		// Nothing Sonarr lists is part of the request. Right after an add that is because
		// Sonarr is still loading the episodes from TVDB (this used to read as 0 of 0 =
		// "complete", so the apps said "Downloaded" and stopped looking). Later it means
		// TVDB has no episodes for the asked-for seasons yet: waiting for them to be listed.
		d.Have, d.Wanted = 0, 0
		if len(eps) == 0 && recentlySent(r, now) {
			d.State, d.Message = DLSearching, "Sonarr is loading the episode list."
		} else {
			d.State, d.WaitingFor = DLWaiting, "airing"
		}
		return d
	}
	noteRetry(d, history, now, func(id int) bool { _, ok := byID[id]; return ok })
	searching := beingSearched(cmds, r.ArrID, byID)
	d.Episodes = episodeStates(byID, d.Items, searching, now)
	for _, e := range d.Episodes {
		d.CanSearch = d.CanSearch || e.CanSearch
		d.CanRetry = d.CanRetry || e.CanRetry
	}
	// Nothing downloading yet but a search is running: say so, rather than "not found".
	if len(searching) > 0 && (d.State == DLNotFound || d.State == DLSearching) {
		d.State, d.Message = DLSearching, "Sonarr is searching right now."
	}
	return d
}

// allHaveFiles: every episode of a download already has a file (it is a replacement).
func allHaveFiles(eps []arr.Episode, byID map[int]arr.Episode) bool {
	for _, e := range eps {
		if cur, ok := byID[e.ID]; !ok || !cur.HasFile {
			return false
		}
	}
	return true
}

// upgradeMessage: "Upgrading S1 E2 to a better release (73%)."
func upgradeMessage(items []DownloadItem) string {
	var eps []arr.Episode
	var prog float64
	for _, it := range items {
		eps = append(eps, it.eps...)
		prog += it.Progress
	}
	return fmt.Sprintf("Upgrading %s to a better release (%d%%).", episodesLabel(eps), int(prog/float64(len(items))*100))
}

// recentlySent: the request was sent (or widened) in the last few minutes.
func recentlySent(r *store.Request, now time.Time) bool {
	for _, v := range []string{r.UpdatedAt, r.CreatedAt} {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return now.Sub(t) < loadingGrace
		}
	}
	return false
}

// loadingGrace: how long a new series may show no episodes before it counts as waiting.
const loadingGrace = 10 * time.Minute

// episodeStates gives every requested episode without a file its own state: from the
// download it is part of, else failed / not found from the items, else waiting for its
// air date, else searching.
func episodeStates(byID map[int]arr.Episode, items []DownloadItem, searching map[int]bool, now time.Time) []EpisodeDL {
	from := map[int]DownloadItem{}
	for _, it := range items {
		for _, id := range it.episodeIDs {
			if cur, ok := from[id]; !ok || rank(it.State) > rank(cur.State) {
				from[id] = it
			}
		}
	}
	var out []EpisodeDL
	for id, e := range byID {
		if e.HasFile {
			continue
		}
		st := EpisodeDL{Season: e.SeasonNumber, Episode: e.EpisodeNumber}
		it, queued := from[id]
		inClient := queued && len(it.queueIDs) > 0
		switch {
		case queued:
			st.State, st.Progress, st.TimeLeftSec, st.Message, st.CanRetry = it.State, it.Progress, it.TimeLeftSec, it.Message, it.CanRetry
		case !e.Aired(now):
			st.State = DLWaiting
			if e.AirDateUTC != nil {
				st.AirDate = e.AirDateUTC.Format(time.RFC3339)
			}
		case e.LastSearchTime != nil:
			st.State = DLNotFound // searched, nothing grabbed: not "searching" unless a search runs (below)
		default:
			st.State = DLSearching
		}
		if searching[id] && !inClient {
			st.State, st.Message, st.CanRetry = DLSearching, "Sonarr is searching right now.", false
		}
		// Search: aired, not in the download client (even a briefly stalled download is
		// left alone), not being searched now, not searched in the last few minutes.
		st.CanSearch = e.Aired(now) && !inClient && !searching[id] && cooledDown(e.LastSearchTime, now)
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

// codeSet turns "S1E3"-style codes into a lookup; empty means "everything".
func codeSet(codes []string) map[string]bool {
	m := map[string]bool{}
	for _, c := range codes {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			m[c] = true
		}
	}
	return m
}

func epCode(season, episode int) string { return fmt.Sprintf("S%dE%d", season, episode) }

func (s *Service) movieStatus(r *store.Request, m *arr.Movie, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
	return s.movieStatusWith(r, m, queue, history, nil, now)
}

func (s *Service) movieStatusWith(r *store.Request, m *arr.Movie, queue []arr.QueueItem, history []arr.HistoryRecord, cmds []arr.Command, now time.Time) *Download {
	d := s.movieStatusBase(r, m, queue, history, now)
	searching := false
	for _, cmd := range cmds {
		searching = searching || cmd.Covers(0, arr.Episode{}, m.ID)
	}
	inClient := false
	for _, it := range d.Items {
		inClient = inClient || len(it.queueIDs) > 0
		d.CanRetry = d.CanRetry || it.CanRetry
	}
	d.CanSearch = d.State != DLComplete && m.IsAvailable && !inClient && !searching && cooledDown(m.LastSearchTime, now)
	if searching && !inClient && d.State != DLComplete {
		d.State, d.Message, d.CanRetry = DLSearching, "Radarr is searching right now.", false
	}
	return d
}

func (s *Service) movieStatusBase(r *store.Request, m *arr.Movie, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
	d := &Download{UpdatedAt: now, LastSearch: m.LastSearchTime}
	if m.HasFile {
		d.Have, d.Wanted = 1, 1
		d.State = DLComplete
		return d
	}
	if !m.IsAvailable {
		for _, t := range []*time.Time{m.DigitalRelease, m.PhysicalRelease, m.InCinemas} {
			if t != nil && t.After(now) && (d.NextAir == nil || t.Before(*d.NextAir)) {
				tt := *t
				d.NextAir = &tt
			}
		}
		d.Upcoming = 1
		d.WaitingFor = "release"
		if m.InCinemas != nil && !m.InCinemas.After(now) {
			d.WaitingFor = "digital" // TMDB says "Released": it is, in cinemas
		}
	} else {
		d.Wanted = 1
	}
	groups := map[string][]arr.QueueItem{}
	var order []string
	for _, q := range queue {
		key := q.DownloadID
		if key == "" {
			key = fmt.Sprintf("q%d", q.ID)
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], q)
	}
	for _, key := range order {
		it := s.queueItem(key, groups[key], nil, now)
		it.Label = r.Title
		d.Items = append(d.Items, it)
	}
	missing, notFound := 0, 0
	if len(d.Items) == 0 && d.Wanted == 1 {
		missing = 1
		// The latest of grabbed / downloadFailed decides; a failure Radarr already
		// replaced with a new grab is not a failure any more.
		for _, h := range history {
			if h.EventType == "grabbed" {
				break
			}
			if h.EventType == "downloadFailed" {
				d.Items = append(d.Items, DownloadItem{Label: r.Title, State: DLFailed, Message: h.Data["message"], CanRetry: true})
				break
			}
		}
		if len(d.Items) == 0 && m.LastSearchTime != nil {
			grabbedSince := false
			for _, h := range history {
				if h.EventType == "grabbed" && h.Date.After(*m.LastSearchTime) {
					grabbedSince = true
				}
			}
			if !grabbedSince {
				notFound = 1
			}
		}
		// Grabbed a moment ago and not in the queue yet: on its way, not "not found".
		if len(d.Items) == 0 {
			for _, h := range history {
				if h.EventType == "downloadFailed" || h.EventType == "downloadFolderImported" {
					break
				}
				if justGrabbed(h, now) {
					d.Items = append(d.Items, DownloadItem{Label: r.Title, State: DLQueued, Message: sentToClient})
					notFound = 0
					break
				}
			}
		}
	}
	s.summarise(d, missing, notFound)
	noteRetry(d, history, now, func(int) bool { return true })
	return d
}

// A search that finds a release grabs it and finishes; the download shows in the arr's
// queue only when it next polls the download client (seconds, up to a minute). In
// between, "searched, nothing in the queue" read as "not found" for a release that was
// just grabbed. A grab this recent counts as queued.
const grabGrace = 15 * time.Minute

const sentToClient = "Sent to the download client; waiting for it to start."

func justGrabbed(h arr.HistoryRecord, now time.Time) bool {
	return h.EventType == "grabbed" && now.Sub(h.Date) < grabGrace
}

// noteRetry: Sonarr / Radarr replace a failed download with another release on their
// own, within seconds. Say so while that happens rather than hiding the failure.
func noteRetry(d *Download, history []arr.HistoryRecord, now time.Time, ours func(episodeID int) bool) {
	if d.Message != "" {
		return
	}
	switch d.State {
	case DLDownloading, DLQueued, DLSearching, DLImporting:
	default:
		return
	}
	for _, h := range history { // newest first
		if now.Sub(h.Date) > 24*time.Hour {
			return
		}
		if h.EventType != "downloadFailed" || !ours(h.EpisodeID) {
			continue
		}
		d.Message = "Trying another release: the last one failed."
		if m := h.Data["message"]; strings.Contains(strings.ToLower(m), "manually marked as failed") {
			d.Message = "Trying another release, as asked." // our own Try another release
		} else if m != "" {
			d.Message = "Trying another release: the last one failed (" + m + ")."
		}
		return
	}
}

// queueItem turns the records of one download into an item, with Roxy's own stall check.
func (s *Service) queueItem(key string, recs []arr.QueueItem, byID map[int]arr.Episode, now time.Time) DownloadItem {
	q := recs[0]
	it := DownloadItem{Release: q.Title}
	var epis []arr.Episode
	for _, r := range recs {
		it.queueIDs = append(it.queueIDs, r.ID)
		if r.EpisodeID != 0 {
			it.episodeIDs = append(it.episodeIDs, r.EpisodeID)
			if e, ok := byID[r.EpisodeID]; ok {
				epis = append(epis, e)
			} else if r.Episode != nil {
				epis = append(epis, arr.Episode{ID: r.EpisodeID, SeasonNumber: r.Episode.SeasonNumber, EpisodeNumber: r.Episode.EpisodeNumber})
			}
		}
	}
	it.Label = episodesLabel(epis)
	it.eps = epis
	// Every record of a pack carries the whole download's size.
	size, left := q.Size, q.SizeLeft
	if size > 0 {
		it.Progress = clamp01((size - left) / size)
	}
	if t := q.TimeLeftSeconds(); t > 0 {
		it.TimeLeftSec = t
	}
	it.State, it.Message = queueState(q)
	// The client's own stall warning ("stalled with no connections"), as opposed to paused
	// or an unreachable client: a fresh torrent shows it for its first moments while it
	// finds peers.
	clientWarning := it.State == DLStalled && (q.Status == "warning" || q.TrackedDownloadStatus == "warning")
	// No bytes moved for a while: stalled, whatever the client says.
	s.dl.mu.Lock()
	mark, ok := s.dl.progress[key]
	if !ok || mark.left != left {
		s.dl.progress[key] = progressMark{left: left, since: now}
	} else if it.State == DLDownloading && left > 0 && now.Sub(mark.since) >= stallAfter {
		it.State = DLStalled
		it.Message = fmt.Sprintf("No progress for %d minutes.", int(now.Sub(mark.since).Minutes()))
	}
	// Failed or unimportable: replace at once. Stalled: only once it has lasted, since a
	// stalled download often picks up again by itself.
	switch it.State {
	case DLFailed, DLAttention:
		it.CanRetry = true
		delete(s.dl.stalled, key)
	case DLStalled:
		first, ok := s.dl.stalled[key]
		if !ok {
			first = now
			s.dl.stalled[key] = now
		}
		it.CanRetry = now.Sub(first) >= stallRetryAfter
		if clientWarning && now.Sub(first) < stallQuiet {
			// Too early to call it a problem: say what is happening instead.
			it.State, it.Message = DLQueued, "Looking for peers…"
			if it.Progress > 0 {
				it.State, it.Message = DLDownloading, ""
			}
		} else if !it.CanRetry && it.Message != "" {
			it.Message += fmt.Sprintf(" Giving it %d more min to recover.", int((stallRetryAfter-now.Sub(first)).Minutes())+1)
		}
	default:
		delete(s.dl.stalled, key)
	}
	s.dl.mu.Unlock()
	return it
}

// queueState maps the arr's queue fields onto Roxy's states.
func queueState(q arr.QueueItem) (string, string) {
	msg := q.Messages()
	switch q.TrackedDownloadState {
	case "failedPending", "failed":
		return DLFailed, msg
	case "importBlocked":
		return DLAttention, msg
	case "importPending", "importing", "imported":
		return DLImporting, ""
	}
	switch {
	case q.Status == "failed" || q.TrackedDownloadStatus == "error":
		return DLFailed, msg
	case q.Status == "warning" || q.TrackedDownloadStatus == "warning":
		return DLStalled, msg
	}
	switch q.Status {
	case "downloading":
		return DLDownloading, ""
	case "completed":
		return DLImporting, ""
	case "paused":
		return DLStalled, "Paused in the download client."
	case "delay":
		return DLQueued, "Held back for a while by a delay profile."
	case "downloadClientUnavailable":
		return DLStalled, "The download client is not reachable."
	}
	return DLQueued, ""
}

// summarise picks the headline state, progress and time left from the items.
func (s *Service) summarise(d *Download, missing, notFound int) {
	var size, done float64
	worst := ""
	for _, it := range d.Items {
		if rank(it.State) > rank(worst) {
			worst = it.State
			if it.Message != "" {
				d.Message = it.Message
			}
		}
		if it.State == DLDownloading || it.State == DLStalled || it.State == DLQueued || it.State == DLImporting {
			size++
			done += it.Progress
			if it.TimeLeftSec > d.TimeLeftSec {
				d.TimeLeftSec = it.TimeLeftSec
			}
		}
	}
	if size > 0 {
		d.Progress = done / size
	}
	d.Groups = groupItems(d.Items)
	switch {
	case worst != "":
		d.State = worst
	case d.Wanted > 0 && d.Have >= d.Wanted:
		d.State = DLComplete
	case d.Wanted == 0 && d.Upcoming > 0:
		d.State = DLWaiting
		if d.WaitingFor == "" {
			d.WaitingFor = "airing"
		}
	case d.Wanted == 0 && d.Have == 0:
		d.State = DLComplete // nothing aired is requested (e.g. a future season with no dates)
	case notFound > 0 && notFound >= missing:
		d.State = DLNotFound
	case d.LastSearch != nil:
		// Searched and nothing is in the client: "not found yet", not "searching";
		// searching is only shown while a search actually runs (see seriesStatusWith).
		d.State = DLNotFound
	default:
		d.State = DLSearching // just sent, no search has finished yet
	}
}

// groupItems folds items by state, worst state first.
func groupItems(items []DownloadItem) []DownloadGroup {
	type acc struct {
		g    DownloadGroup
		eps  []arr.Episode
		prog float64
		lbl  string
	}
	by := map[string]*acc{}
	var order []string
	for _, it := range items {
		a := by[it.State]
		if a == nil {
			a = &acc{g: DownloadGroup{State: it.State}, lbl: it.Label}
			by[it.State] = a
			order = append(order, it.State)
		}
		a.g.Count++
		a.eps = append(a.eps, it.eps...)
		a.prog += it.Progress
		if it.TimeLeftSec > a.g.TimeLeftSec {
			a.g.TimeLeftSec = it.TimeLeftSec
		}
		if a.g.Message == "" {
			a.g.Message = it.Message
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return rank(order[i]) > rank(order[j]) })
	out := make([]DownloadGroup, 0, len(order))
	for _, st := range order {
		a := by[st]
		a.g.Progress = a.prog / float64(a.g.Count)
		a.g.Label = a.lbl
		if len(a.eps) > 0 {
			a.g.Label = episodesLabel(a.eps)
		}
		out = append(out, a.g)
	}
	return out
}

// latestEvents: per episode, the newest grabbed / downloadFailed / imported event.
func latestEvents(history []arr.HistoryRecord) map[int]arr.HistoryRecord {
	out := map[int]arr.HistoryRecord{}
	for _, h := range history {
		switch h.EventType {
		case "grabbed", "downloadFailed", "downloadFolderImported", "seriesFolderImported":
		default:
			continue
		}
		if cur, ok := out[h.EpisodeID]; !ok || h.Date.After(cur.Date) {
			out[h.EpisodeID] = h
		}
	}
	return out
}

// episodesLabel: "S1 E3", "S1 E3–E5", "Season 1 · 12 episodes", "14 episodes".
func episodesLabel(eps []arr.Episode) string {
	switch len(eps) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("S%d E%d", eps[0].SeasonNumber, eps[0].EpisodeNumber)
	}
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].SeasonNumber != eps[j].SeasonNumber {
			return eps[i].SeasonNumber < eps[j].SeasonNumber
		}
		return eps[i].EpisodeNumber < eps[j].EpisodeNumber
	})
	season := eps[0].SeasonNumber
	for _, e := range eps {
		if e.SeasonNumber != season {
			return fmt.Sprintf("%d episodes", len(eps))
		}
	}
	consecutive := eps[len(eps)-1].EpisodeNumber-eps[0].EpisodeNumber == len(eps)-1
	if consecutive && len(eps) <= 3 {
		return fmt.Sprintf("S%d E%d–E%d", season, eps[0].EpisodeNumber, eps[len(eps)-1].EpisodeNumber)
	}
	return fmt.Sprintf("Season %d · %d episodes", season, len(eps))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---- actions ----

// SearchAgain asks the arr app to look for the request's missing episodes (or the movie)
// now; with seasons and / or codes ("S1E3"), only those seasons and episodes.
func (s *Service) SearchAgain(ctx context.Context, requestID string, seasons []int, codes ...string) error {
	r, c, err := s.trackedRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if r.MediaType == "movie" {
		if err = s.movieSearchable(ctx, c, r); err != nil {
			return err
		}
		err = c.SearchMovie(ctx, r.ArrID)
	} else {
		want := codeSet(codes)
		err = s.searchMissing(ctx, c, r, func(ep arr.Episode) bool {
			if len(want) == 0 && len(seasons) == 0 {
				return true
			}
			return want[epCode(ep.SeasonNumber, ep.EpisodeNumber)] || contains(seasons, ep.SeasonNumber)
		})
	}
	if err == nil {
		s.markSearching(requestID, seasons, codes...)
		s.KickDownloads()
	}
	return err
}

// searchMissing searches every aired, requested episode without a file that pick accepts,
// leaving out what is in the download client (even a stalled download: it may recover),
// what Sonarr is searching for right now, and what was searched in the last few minutes.
func (s *Service) searchMissing(ctx context.Context, c *arr.Client, r *store.Request, pick func(arr.Episode) bool) error {
	eps, err := c.Episodes(ctx, r.ArrID)
	if err != nil {
		return err
	}
	queue, err := c.Queue(ctx, r.ArrID)
	if err != nil {
		return err
	}
	cmds, _ := c.ActiveSearches(ctx)
	inClient := map[int]bool{}
	for _, q := range queue {
		if st, _ := queueState(q); st != DLFailed {
			inClient[q.EpisodeID] = true
		}
	}
	var ids []int
	var busy, recent, picked int
	var soonest time.Duration
	now := time.Now()
	seasons := map[int]*seasonTally{}
	for _, ep := range eps {
		if !inScope(r, ep) || !ep.Aired(now) || !pick(ep) {
			continue
		}
		t := seasons[ep.SeasonNumber]
		if t == nil {
			t = &seasonTally{}
			seasons[ep.SeasonNumber] = t
		}
		t.aired++
		if ep.HasFile {
			continue
		}
		picked++
		covered := false
		for _, cmd := range cmds {
			covered = covered || cmd.Covers(r.ArrID, ep, 0)
		}
		switch {
		case inClient[ep.ID] || covered:
			busy++
		case !cooledDown(ep.LastSearchTime, now):
			recent++
			if wait := searchCooldown - now.Sub(*ep.LastSearchTime); soonest == 0 || wait < soonest {
				soonest = wait
			}
		default:
			ids = append(ids, ep.ID)
			t.ids = append(t.ids, ep.ID)
		}
	}
	if len(ids) == 0 {
		switch {
		case picked == 0:
			return fmt.Errorf("nothing there is missing")
		case recent > 0 && busy == 0:
			return fmt.Errorf("searched a moment ago; nothing new turns up that fast. Try again in %d min", int(soonest.Minutes())+1)
		default:
			return fmt.Errorf("Sonarr is already on it: downloading or searching right now")
		}
	}
	// A season with every aired episode missing gets one SeasonSearch: episode searches
	// never match a season pack, so they grab single episodes (often bigger, or a stray
	// that then blocks the pack as a downgrade).
	whole, rest := splitSeasonSearches(seasons)
	for _, n := range whole {
		if err := c.SearchSeason(ctx, r.ArrID, n); err != nil {
			return err
		}
	}
	return c.SearchEpisodes(ctx, rest)
}

// seasonTally: a season's aired, requested episodes, and the ones free to search now.
type seasonTally struct {
	aired int
	ids   []int
}

// splitSeasonSearches returns the seasons to search whole (more than one aired episode,
// every one of them missing and searchable) and the episode ids left to search singly.
func splitSeasonSearches(seasons map[int]*seasonTally) (whole []int, rest []int) {
	for n, t := range seasons {
		if n > 0 && t.aired > 1 && len(t.ids) == t.aired {
			whole = append(whole, n)
		} else {
			rest = append(rest, t.ids...)
		}
	}
	sort.Ints(whole)
	sort.Ints(rest)
	return whole, rest
}

// movieSearchable: the same rules for a movie.
func (s *Service) movieSearchable(ctx context.Context, c *arr.Client, r *store.Request) error {
	m, err := c.Movie(ctx, r.ArrID)
	if err != nil {
		return err
	}
	if m.HasFile {
		return fmt.Errorf("it is already downloaded")
	}
	queue, _ := c.Queue(ctx, r.ArrID)
	for _, q := range queue {
		if st, _ := queueState(q); st != DLFailed {
			return fmt.Errorf("Radarr is already downloading it")
		}
	}
	cmds, _ := c.ActiveSearches(ctx)
	for _, cmd := range cmds {
		if cmd.Covers(0, arr.Episode{}, m.ID) {
			return fmt.Errorf("Radarr is searching for it right now")
		}
	}
	if !cooledDown(m.LastSearchTime, time.Now()) {
		return fmt.Errorf("searched a moment ago; nothing new turns up that fast. Try again in %d min", int((searchCooldown-time.Since(*m.LastSearchTime)).Minutes())+1)
	}
	return nil
}

// searchSeasonsAgain: a season that was already requested and is asked for again gets
// its missing episodes searched (a request that changes nothing used to do nothing). The
// error says why nothing was searched ("searched a moment ago", "already on it"), for the
// person who asked.
func (s *Service) searchSeasonsAgain(ctx context.Context, r *store.Request, seasons []int) error {
	sonarr, _ := s.clients()
	if sonarr == nil || r.ArrID == 0 {
		return nil
	}
	if err := s.searchMissing(ctx, sonarr, r, func(ep arr.Episode) bool { return len(seasons) == 0 || contains(seasons, ep.SeasonNumber) }); err != nil {
		slog.Info("discover: search again", "title", r.Title, "err", err)
		return err
	}
	s.markSearching(r.ID, seasons)
	s.KickDownloads()
	return nil
}

// TryAnotherRelease removes failed, stalled or unimportable downloads from the client,
// blocklists those releases and lets the arr app pick another; failed episodes that are
// no longer queued are searched again.
func (s *Service) TryAnotherRelease(ctx context.Context, requestID string, codes ...string) error {
	r, c, err := s.trackedRequest(ctx, requestID)
	if err != nil {
		return err
	}
	d := s.Download(requestID)
	if d == nil {
		return fmt.Errorf("no download status yet")
	}
	want := codeSet(codes)
	var queueIDs, searchIDs []int
	for _, it := range d.Items {
		if len(want) > 0 {
			hit := false
			for _, e := range it.eps {
				hit = hit || want[epCode(e.SeasonNumber, e.EpisodeNumber)]
			}
			if !hit {
				continue
			}
		}
		if !it.CanRetry {
			continue // a short stall gets time to recover; healthy downloads are left alone
		}
		switch it.State {
		case DLFailed, DLStalled, DLAttention:
			if len(it.queueIDs) > 0 {
				queueIDs = append(queueIDs, it.queueIDs...)
			} else {
				searchIDs = append(searchIDs, it.episodeIDs...)
			}
		}
	}
	if len(queueIDs) == 0 && len(searchIDs) == 0 {
		for _, it := range d.Items {
			if it.State == DLStalled {
				return fmt.Errorf("that download only just stalled; it gets %d minutes to recover before it is replaced", int(stallRetryAfter.Minutes()))
			}
		}
		return s.SearchAgain(ctx, requestID, nil, codes...)
	}
	if err := c.RemoveAndBlocklist(ctx, queueIDs); err != nil {
		return err
	}
	if r.MediaType == "movie" && len(queueIDs) == 0 {
		err = c.SearchMovie(ctx, r.ArrID)
	} else {
		err = c.SearchEpisodes(ctx, searchIDs)
	}
	if err == nil {
		s.markSearching(requestID, nil, codes...)
		s.KickDownloads()
	}
	return err
}

// markSearching shows the search at once (the next poll fills in the truth): the whole
// request, or just the given seasons and episodes.
func (s *Service) markSearching(requestID string, seasons []int, codes ...string) {
	s.dl.mu.Lock()
	defer s.dl.mu.Unlock()
	d := s.dl.status[requestID]
	if d == nil {
		return
	}
	c := *d
	c.UpdatedAt = time.Now()
	want := codeSet(codes)
	if len(want) == 0 && len(seasons) == 0 {
		c.State, c.Message, c.Items, c.Groups = DLSearching, "", nil, nil
		eps := make([]EpisodeDL, len(c.Episodes))
		for i, e := range c.Episodes {
			if e.State != DLWaiting {
				e.State, e.Progress, e.Message = DLSearching, 0, ""
			}
			eps[i] = e
		}
		c.Episodes = eps
	} else {
		eps := make([]EpisodeDL, len(c.Episodes))
		for i, e := range c.Episodes {
			if want[epCode(e.Season, e.Episode)] || (contains(seasons, e.Season) && e.State != DLWaiting) {
				e.State, e.Progress, e.Message = DLSearching, 0, ""
			}
			eps[i] = e
		}
		c.Episodes = eps
	}
	s.dl.status[requestID] = &c
}

func (s *Service) trackedRequest(ctx context.Context, requestID string) (*store.Request, *arr.Client, error) {
	all, err := s.store.ListRequests(ctx)
	if err != nil {
		return nil, nil, err
	}
	sonarr, radarr := s.clients()
	for i := range all {
		r := &all[i]
		if r.ID != requestID {
			continue
		}
		if r.Status != "sent" || r.ArrID == 0 {
			return nil, nil, fmt.Errorf("this request has not reached %s yet", arrName(r.MediaType))
		}
		c := sonarr
		if r.MediaType == "movie" {
			c = radarr
		}
		if c == nil {
			return nil, nil, fmt.Errorf("%s isn't connected", arrName(r.MediaType))
		}
		return r, c, nil
	}
	return nil, nil, fmt.Errorf("request not found")
}

func arrName(mediaType string) string {
	if mediaType == "movie" {
		return "Radarr"
	}
	return "Sonarr"
}
