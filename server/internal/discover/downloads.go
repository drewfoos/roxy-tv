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
// request app reads the first page (10 records) of the arr queue every minute and shows a
// progress bar on hover; it never says when a download stalls, fails, cannot be
// imported, or when nothing was found. Here every sent request gets a status built from
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
	// Groups is Items folded by state for display: "Downloading · Season 1 · 10 episodes".
	Groups    []DownloadGroup `json:"groups,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
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
	Label       string  `json:"label"` // "S01E03", "Season 1 · 12 episodes", the movie title
	Release     string  `json:"release,omitempty"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	TimeLeftSec int     `json:"timeLeftSec,omitempty"`
	Message     string  `json:"message,omitempty"`
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
}

type progressMark struct {
	left  float64
	since time.Time
}

const stallAfter = 10 * time.Minute

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
		if prev != nil && prev.State == DLComplete && prev.Upcoming == 0 && time.Since(prev.UpdatedAt) < 10*time.Minute {
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
	now := time.Now()
	if c.Name == "radarr" {
		m, err := c.Movie(ctx, r.ArrID)
		if err != nil {
			return nil, err
		}
		return s.movieStatus(r, m, queue, history, now), nil
	}
	eps, err := c.Episodes(ctx, r.ArrID)
	if err != nil {
		return nil, err
	}
	return s.seriesStatus(r, eps, queue, history, now), nil
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
	var failed, notFound []arr.Episode
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
		if e.LastSearchTime != nil {
			if ev, ok := last[e.ID]; !ok || ev.Date.Before(*e.LastSearchTime) {
				notFound = append(notFound, e)
			}
		}
	}
	if len(failed) > 0 {
		it := DownloadItem{Label: episodesLabel(failed), State: DLFailed, Message: failMsg, eps: failed}
		for _, e := range failed {
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
	s.summarise(d, len(missing), len(notFound))
	noteRetry(d, history, now, func(id int) bool { _, ok := byID[id]; return ok })
	return d
}

func (s *Service) movieStatus(r *store.Request, m *arr.Movie, queue []arr.QueueItem, history []arr.HistoryRecord, now time.Time) *Download {
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
				d.Items = append(d.Items, DownloadItem{Label: r.Title, State: DLFailed, Message: h.Data["message"]})
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
	}
	s.summarise(d, missing, notFound)
	noteRetry(d, history, now, func(int) bool { return true })
	return d
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
	// No bytes moved for a while: stalled, whatever the client says.
	s.dl.mu.Lock()
	mark, ok := s.dl.progress[key]
	if !ok || mark.left != left {
		s.dl.progress[key] = progressMark{left: left, since: now}
	} else if it.State == DLDownloading && left > 0 && now.Sub(mark.since) >= stallAfter {
		it.State = DLStalled
		it.Message = fmt.Sprintf("No progress for %d minutes.", int(now.Sub(mark.since).Minutes()))
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
	case d.Wanted == 0 && d.Have == 0:
		d.State = DLComplete // nothing aired is requested (e.g. a future season with no dates)
	case notFound > 0 && notFound >= missing:
		d.State = DLNotFound
	default:
		d.State = DLSearching
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

// episodesLabel: "S01E03", "S01E03–E05", "Season 1 · 12 episodes", "14 episodes".
func episodesLabel(eps []arr.Episode) string {
	switch len(eps) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("S%02dE%02d", eps[0].SeasonNumber, eps[0].EpisodeNumber)
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
		return fmt.Sprintf("S%02dE%02d–E%02d", season, eps[0].EpisodeNumber, eps[len(eps)-1].EpisodeNumber)
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

// SearchAgain asks the arr app to look for the request's missing episodes (or the movie) now.
func (s *Service) SearchAgain(ctx context.Context, requestID string) error {
	r, c, err := s.trackedRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if r.MediaType == "movie" {
		err = c.SearchMovie(ctx, r.ArrID)
	} else {
		eps, e := c.Episodes(ctx, r.ArrID)
		if e != nil {
			return e
		}
		var ids []int
		now := time.Now()
		for _, ep := range eps {
			if inScope(r, ep) && ep.Aired(now) && !ep.HasFile {
				ids = append(ids, ep.ID)
			}
		}
		if len(ids) == 0 {
			return fmt.Errorf("nothing is missing")
		}
		err = c.SearchEpisodes(ctx, ids)
	}
	if err == nil {
		s.markSearching(requestID)
		s.KickDownloads()
	}
	return err
}

// TryAnotherRelease removes failed, stalled or unimportable downloads from the client,
// blocklists those releases and lets the arr app pick another; failed episodes that are
// no longer queued are searched again.
func (s *Service) TryAnotherRelease(ctx context.Context, requestID string) error {
	r, c, err := s.trackedRequest(ctx, requestID)
	if err != nil {
		return err
	}
	d := s.Download(requestID)
	if d == nil {
		return fmt.Errorf("no download status yet")
	}
	var queueIDs, searchIDs []int
	for _, it := range d.Items {
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
		return s.SearchAgain(ctx, requestID)
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
		s.markSearching(requestID)
		s.KickDownloads()
	}
	return err
}

func (s *Service) markSearching(requestID string) {
	s.dl.mu.Lock()
	if d := s.dl.status[requestID]; d != nil {
		c := *d
		c.State, c.Message, c.Items = DLSearching, "", nil
		c.UpdatedAt = time.Now()
		s.dl.status[requestID] = &c
	}
	s.dl.mu.Unlock()
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
