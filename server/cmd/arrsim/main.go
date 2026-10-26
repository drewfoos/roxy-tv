// Command arrsim is a development stand-in for the two things Sonarr and Radarr need
// to "download" something: a Newznab indexer and a SABnzbd download client. It lets
// real Sonarr / Radarr instances run through search, grab, download, import and failure
// on a laptop with no indexers, no Usenet account and nothing fetched from the
// internet: every release it offers is fake, and every "download" is a small local test
// video it writes itself (or a broken folder, when told to fail).
//
// It exists to test Roxy's request tracking (searching, downloading with progress,
// stalled, failed, importing, available) end to end. Never point a real arr app that
// matters at it.
//
//	go run ./cmd/arrsim -listen :8099 -downloads /data/downloads -report-dir /downloads \
//	    -video sample.mkv -sonarr http://localhost:8989 -sonarr-key KEY -radarr ... -radarr-key ...
//
// Indexer:  http://host:8099/newznab/api   (any API key)
// SABnzbd:  host:8099, URL base /sabnzbd, API key "arrsim"
// Control:  POST /sim/mode?value=ok|slow|stall|fail|badimport|none  (what the next grabs do)
//
//	GET  /sim/state                                    (current downloads, as JSON)
package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	listen     = flag.String("listen", ":8099", "address to listen on")
	downloads  = flag.String("downloads", "/tmp/arrsim", "folder finished downloads are written to (as this process sees it)")
	reportDir  = flag.String("report-dir", "", "the same folder as Sonarr/Radarr see it (default: -downloads)")
	video      = flag.String("video", "", "a small video file copied in as each downloaded episode or movie")
	sonarrURL  = flag.String("sonarr", "", "Sonarr URL, to turn TVDB ids into titles")
	sonarrKey  = flag.String("sonarr-key", "", "Sonarr API key")
	radarrURL  = flag.String("radarr", "", "Radarr URL, to turn TMDB ids into titles")
	radarrKey  = flag.String("radarr-key", "", "Radarr API key")
	downloadTo = flag.Duration("duration", 90*time.Second, "how long an ok download takes")
	slots      = flag.Int("slots", 3, "downloads at once; the rest wait as Queued, like a real client")
)

// ---- simulated downloads ----

type job struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
	Mode     string    `json:"mode"` // ok | slow | stall | fail | badimport
	Size     int64     `json:"size"`
	Started  time.Time `json:"started"` // when it got a download slot (zero while queued)
	Files    []string  `json:"files"`   // episode file names inside the folder ("" for a movie: one file)
	Done     bool      `json:"done"`
	Failed   string    `json:"failed,omitempty"`
	Deleted  bool      `json:"deleted"`
}

func (j *job) progress(now time.Time) float64 {
	if j.Started.IsZero() {
		return 0 // still waiting for a slot
	}
	d := *downloadTo
	switch j.Mode {
	case "slow":
		d *= 4
	case "stall":
		// Gets to 37% and stays there.
		p := float64(now.Sub(j.Started)) / float64(d)
		if p > 0.37 {
			p = 0.37
		}
		return p
	}
	p := float64(now.Sub(j.Started)) / float64(d)
	if p > 1 {
		p = 1
	}
	return p
}

var (
	mu      sync.Mutex
	mode    = "ok"
	jobs    []*job
	nextID  = 1
	nzbMeta = map[string]release{} // nzb id -> release, so the grab knows what it is
	// runID keeps download ids unique across restarts: an id the arr app has already seen
	// (and imported) makes it treat a new download as the old one and drop it.
	runID = strconv.FormatInt(time.Now().Unix()%1_000_000, 36)
)

type release struct {
	ID       string
	Title    string
	Category string
	Size     int64
	Files    []string
}

// ---- titles from the arr apps ----

// arrHTTP has a timeout: the arr app is itself waiting on this indexer while we ask it
// for a title, and a lookup that never returns would hang its search for good.
var arrHTTP = &http.Client{Timeout: 8 * time.Second}

func arrGet(base, key, path string, q url.Values, out any) error {
	if base == "" {
		return fmt.Errorf("no url")
	}
	u := strings.TrimRight(base, "/") + "/api/v3" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("X-Api-Key", key)
	resp, err := arrHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type seriesInfo struct {
	Title   string
	Seasons map[int]int // season -> episode count
}

func seriesByTVDB(tvdb string) *seriesInfo {
	var list []struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Seasons []struct {
			SeasonNumber int `json:"seasonNumber"`
			Statistics   struct {
				TotalEpisodeCount int `json:"totalEpisodeCount"`
			} `json:"statistics"`
		} `json:"seasons"`
	}
	if err := arrGet(*sonarrURL, *sonarrKey, "/series", url.Values{"tvdbId": {tvdb}}, &list); err != nil || len(list) == 0 {
		return nil
	}
	s := &seriesInfo{Title: list[0].Title, Seasons: map[int]int{}}
	for _, sn := range list[0].Seasons {
		s.Seasons[sn.SeasonNumber] = sn.Statistics.TotalEpisodeCount
	}
	return s
}

func seriesByTitle(q string) (*seriesInfo, string) {
	var list []struct {
		Title  string `json:"title"`
		TvdbID int    `json:"tvdbId"`
	}
	if err := arrGet(*sonarrURL, *sonarrKey, "/series", nil, &list); err != nil {
		return nil, ""
	}
	norm := func(s string) string {
		return strings.ToLower(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), ""))
	}
	for _, s := range list {
		if norm(s.Title) == norm(q) || strings.Contains(norm(s.Title), norm(q)) {
			return seriesByTVDB(strconv.Itoa(s.TvdbID)), strconv.Itoa(s.TvdbID)
		}
	}
	return nil, ""
}

func movieByTMDB(tmdb string) (string, int) {
	var list []struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	if err := arrGet(*radarrURL, *radarrKey, "/movie", url.Values{"tmdbId": {tmdb}}, &list); err != nil || len(list) == 0 {
		return "", 0
	}
	return list[0].Title, list[0].Year
}

func dotted(s string) string {
	s = strings.NewReplacer("'", "", "’", "").Replace(s)
	s = regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(s, ".")
	return strings.Trim(s, ".")
}

// ---- Newznab indexer ----

type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	NS      string   `xml:"xmlns:newznab,attr"`
	Channel channel  `xml:"channel"`
}
type channel struct {
	Title string `xml:"title"`
	Items []item `xml:"item"`
}
type item struct {
	Title     string    `xml:"title"`
	GUID      string    `xml:"guid"`
	Link      string    `xml:"link"`
	PubDate   string    `xml:"pubDate"`
	Enclosure enclosure `xml:"enclosure"`
	Attrs     []attr    `xml:"newznab:attr"`
}
type enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}
type attr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

const capsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="arrsim"/>
  <limits max="100" default="100"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep,tvdbid"/>
    <movie-search available="yes" supportedParams="q,imdbid,tmdbid"/>
  </searching>
  <categories>
    <category id="2000" name="Movies"><subcat id="2040" name="Movies/HD"/></category>
    <category id="5000" name="TV"><subcat id="5040" name="TV/HD"/><subcat id="5070" name="TV/Anime"/></category>
  </categories>
</caps>`

func newznab(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t := q.Get("t")
	if t != "caps" && t != "get" {
		log.Printf("indexer %s request %v", t, q)
	}
	if t == "caps" {
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, capsXML)
		return
	}
	if t == "get" {
		nzb(w, r)
		return
	}
	mu.Lock()
	m := mode
	mu.Unlock()
	var rels []release
	if m != "none" {
		switch t {
		case "tvsearch", "search":
			rels = tvReleases(q)
		case "movie":
			rels = movieReleases(q)
		}
	}
	// A second release of everything from another "group", slightly smaller (so the first
	// wins), lets blocklist-and-search-again find something new.
	for _, rel := range append([]release(nil), rels...) {
		alt := rel
		alt.ID += "-alt"
		alt.Title = strings.Replace(strings.Replace(rel.Title, "-ArrSim", "-ArrSimB", 1), "[ArrSim]", "[ArrSimB]", 1)
		alt.Size = rel.Size * 97 / 100
		alt.Files = nil
		for _, f := range rel.Files {
			alt.Files = append(alt.Files, strings.Replace(strings.Replace(f, "-ArrSim", "-ArrSimB", 1), "[ArrSim]", "[ArrSimB]", 1))
		}
		rels = append(rels, alt)
	}
	// The arr apps test an indexer with a bare query and refuse one that returns nothing:
	// answer those with a placeholder no real title matches (RSS is off, so never grabbed).
	if q.Get("q") == "" && q.Get("tvdbid") == "" && q.Get("tmdbid") == "" && q.Get("imdbid") == "" && q.Get("season") == "" {
		cat := "5040"
		title := "ArrSim.Placeholder.S01E01.720p.WEB-DL.AAC2.0.H.264-ArrSim"
		if t == "movie" || strings.HasPrefix(q.Get("cat"), "2") {
			cat, title = "2040", "ArrSim.Placeholder.2001.1080p.WEB-DL.DDP5.1.H.264-ArrSim"
		}
		rels = append(rels, release{ID: "placeholder-" + cat, Title: title, Category: cat, Size: 1_000_000_000})
	}
	base := "http://" + r.Host
	out := rss{Version: "2.0", NS: "http://www.newznab.com/DTD/2010/feeds/attributes/", Channel: channel{Title: "arrsim"}}
	mu.Lock()
	for _, rel := range rels {
		nzbMeta[rel.ID] = rel
		out.Channel.Items = append(out.Channel.Items, item{
			Title: rel.Title, GUID: base + "/details/" + rel.ID, Link: base + "/newznab/api?t=get&id=" + rel.ID,
			PubDate:   time.Now().Add(-2 * time.Hour).Format(time.RFC1123Z),
			Enclosure: enclosure{URL: base + "/newznab/api?t=get&id=" + rel.ID, Length: rel.Size, Type: "application/x-nzb"},
			Attrs:     []attr{{"category", rel.Category}, {"size", strconv.FormatInt(rel.Size, 10)}, {"grabs", "100"}},
		})
	}
	mu.Unlock()
	log.Printf("indexer %s %v -> %d releases", t, q, len(rels))
	w.Header().Set("Content-Type", "application/rss+xml")
	io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(out)
}

func tvReleases(q url.Values) []release {
	var s *seriesInfo
	if id := q.Get("tvdbid"); id != "" {
		s = seriesByTVDB(id)
	}
	if s == nil && q.Get("q") != "" {
		s, _ = seriesByTitle(q.Get("q"))
	}
	// Anime: Sonarr searches by absolute episode number, as "q=08" beside the tvdbid or
	// as "q=<title> 08" on a plain search.
	abs := ""
	// "Title S2 03": a season-relative episode; turn it into the absolute number.
	if m := regexp.MustCompile(`^(.*?)\s*S(\d+)\s+(\d{1,4})$`).FindStringSubmatch(strings.TrimSpace(q.Get("q"))); m != nil {
		if s == nil && m[1] != "" {
			s, _ = seriesByTitle(m[1])
		}
		if s != nil {
			season, _ := strconv.Atoi(m[2])
			n, _ := strconv.Atoi(m[3])
			for sn, count := range s.Seasons {
				if sn > 0 && sn < season {
					n += count
				}
			}
			q.Set("q", strconv.Itoa(n))
		}
	}
	if m := regexp.MustCompile(`^(.*?)\s*(\d{1,4})$`).FindStringSubmatch(strings.TrimSpace(q.Get("q"))); m != nil {
		abs = m[2]
		if s == nil && m[1] != "" {
			s, _ = seriesByTitle(m[1])
		}
	}
	if s == nil {
		return nil // RSS sync and unknown titles: nothing
	}
	if abs != "" && q.Get("season") == "" {
		n, _ := strconv.Atoi(abs)
		clean := strings.NewReplacer("'", "", "’", "").Replace(s.Title) // "Journey's" -> "Journeys", as release groups write it
		clean = strings.TrimSpace(regexp.MustCompile(`[^A-Za-z0-9 ]+`).ReplaceAllString(clean, " "))
		clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
		title := fmt.Sprintf("[ArrSim] %s - %02d [1080p WEB-DL]", clean, n)
		return []release{{ID: fmt.Sprintf("anime-%s-%d", dotted(clean), n), Title: title, Category: "5070", Size: 1_300_000_000, Files: []string{title + ".mkv"}}}
	}
	name := dotted(s.Title)
	season, _ := strconv.Atoi(q.Get("season"))
	if q.Get("season") == "" {
		return nil
	}
	eps := s.Seasons[season]
	if ep := q.Get("ep"); ep != "" {
		n, _ := strconv.Atoi(ep)
		title := fmt.Sprintf("%s.S%02dE%02d.1080p.WEB-DL.AAC2.0.H.264-ArrSim", name, season, n)
		return []release{{ID: fmt.Sprintf("tv-%s-%d-%d", name, season, n), Title: title, Category: "5040", Size: 1_300_000_000, Files: []string{title + ".mkv"}}}
	}
	if eps <= 0 {
		return nil
	}
	title := fmt.Sprintf("%s.S%02d.1080p.WEB-DL.AAC2.0.H.264-ArrSim", name, season)
	var files []string
	for n := 1; n <= eps; n++ {
		files = append(files, fmt.Sprintf("%s.S%02dE%02d.1080p.WEB-DL.AAC2.0.H.264-ArrSim.mkv", name, season, n))
	}
	return []release{{ID: fmt.Sprintf("tv-%s-%d", name, season), Title: title, Category: "5040", Size: int64(eps) * 1_300_000_000, Files: files}}
}

func movieReleases(q url.Values) []release {
	title, year := "", 0
	if id := q.Get("tmdbid"); id != "" {
		title, year = movieByTMDB(id)
	}
	if title == "" {
		return nil
	}
	name := fmt.Sprintf("%s.%d.1080p.WEB-DL.DDP5.1.H.264-ArrSim", dotted(title), year)
	return []release{{ID: "mv-" + dotted(title), Title: name, Category: "2040", Size: 6_000_000_000, Files: []string{name + ".mkv"}}}
}

// nzb serves a tiny NZB; the client below never reads its articles.
func nzb(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	w.Header().Set("Content-Type", "application/x-nzb")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.nzb"`)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><head><meta type="name">%s</meta></head>
<file poster="arrsim" date="0" subject="%s"><groups><group>alt.binaries.test</group></groups><segments><segment bytes="1" number="1">%s@arrsim</segment></segments></file></nzb>`, id, id, id)
}

// ---- SABnzbd API ----

func sab(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseMultipartForm(8 << 20)
	q := r.URL.Query()
	for k, v := range r.Form {
		if q.Get(k) == "" && len(v) > 0 {
			q.Set(k, v[0])
		}
	}
	w.Header().Set("Content-Type", "application/json")
	switch q.Get("mode") {
	case "version":
		json.NewEncoder(w).Encode(map[string]any{"version": "4.3.3"})
	case "get_config":
		json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{
			"misc":       map[string]any{"complete_dir": reportPath(""), "enable_tv_sorting": false, "enable_movie_sorting": false, "pre_check": false, "history_retention": ""},
			"categories": []map[string]any{{"name": "tv", "dir": ""}, {"name": "movies", "dir": ""}, {"name": "*", "dir": ""}},
			"sorters":    []any{},
		}})
	case "fullstatus":
		json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"completedir": reportPath("")}})
	case "addfile", "addurl":
		addJob(w, r, q)
	case "queue":
		if q.Get("name") == "delete" {
			deleteJobs(q.Get("value"))
			json.NewEncoder(w).Encode(map[string]any{"status": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"queue": queue()})
	case "history":
		if q.Get("name") == "delete" {
			deleteJobs(q.Get("value"))
			json.NewEncoder(w).Encode(map[string]any{"status": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"history": history(q.Get("category"))})
	default:
		json.NewEncoder(w).Encode(map[string]any{"status": true})
	}
}

func reportPath(name string) string {
	base := *reportDir
	if base == "" {
		base = *downloads
	}
	if name == "" {
		return base
	}
	return filepath.ToSlash(filepath.Join(base, name))
}

var nzbName = regexp.MustCompile(`<meta type="name">([^<]+)</meta>`)

func addJob(w http.ResponseWriter, r *http.Request, q url.Values) {
	id := ""
	if f, _, err := r.FormFile("name"); err == nil {
		b, _ := io.ReadAll(io.LimitReader(f, 1<<20))
		if m := nzbName.FindSubmatch(b); m != nil {
			id = string(m[1])
		}
	}
	if id == "" {
		if u, err := url.Parse(q.Get("name")); err == nil {
			id = u.Query().Get("id")
		}
	}
	mu.Lock()
	rel, ok := nzbMeta[id]
	m := mode
	if !ok {
		rel = release{ID: id, Title: strings.TrimSuffix(q.Get("nzbname"), ".nzb"), Size: 1_000_000_000}
	}
	if n := q.Get("nzbname"); n != "" {
		rel.Title = strings.TrimSuffix(n, ".nzb")
	}
	j := &job{ID: fmt.Sprintf("SABnzbd_nzo_arrsim%s%04d", runID, nextID), Name: rel.Title, Category: q.Get("cat"), Mode: m, Size: rel.Size, Files: rel.Files}
	if m == "none" || m == "" {
		j.Mode = "ok"
	}
	nextID++
	jobs = append(jobs, j)
	mu.Unlock()
	log.Printf("sab: added %s (%s) mode=%s", j.Name, j.ID, j.Mode)
	json.NewEncoder(w).Encode(map[string]any{"status": true, "nzo_ids": []string{j.ID}})
}

func deleteJobs(ids string) {
	mu.Lock()
	defer mu.Unlock()
	for _, id := range strings.Split(ids, ",") {
		for _, j := range jobs {
			if j.ID == id {
				j.Deleted = true
			}
		}
	}
}

// finish turns a job whose time is up into a completed (or failed) one, writing files.
func finish(j *job, now time.Time) {
	if j.Done || j.Mode == "stall" || j.progress(now) < 1 {
		return
	}
	j.Done = true
	switch j.Mode {
	case "fail":
		j.Failed = "Download failed - Out of your server's retention?"
		return
	}
	dir := filepath.Join(*downloads, j.Name)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		log.Printf("mkdir %s: %v", dir, err)
		return
	}
	if j.Mode == "badimport" {
		os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a video"), 0o666)
		return
	}
	files := j.Files
	if len(files) == 0 {
		files = []string{j.Name + ".mkv"}
	}
	for _, f := range files {
		if err := copyFile(*video, filepath.Join(dir, f)); err != nil {
			log.Printf("copy video: %v", err)
		}
	}
	log.Printf("sab: finished %s (%d files)", j.Name, len(files))
}

func copyFile(src, dst string) error {
	if src == "" {
		return os.WriteFile(dst, nil, 0o666)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func queue() map[string]any {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	assignSlots(now)
	var slots []map[string]any
	for _, j := range jobs {
		finish(j, now)
		if j.Done || j.Deleted {
			continue
		}
		if j.Started.IsZero() {
			mb := float64(j.Size) / 1e6
			slots = append(slots, map[string]any{
				"nzo_id": j.ID, "filename": j.Name, "cat": j.Category, "status": "Queued", "priority": "Normal",
				"mb": fmt.Sprintf("%.2f", mb), "mbleft": fmt.Sprintf("%.2f", mb), "percentage": "0", "timeleft": "0:00:00",
			})
			continue
		}
		p := j.progress(now)
		mb := float64(j.Size) / 1e6
		left := mb * (1 - p)
		status := "Downloading"
		timeleft := "0:00:00"
		if j.Mode == "stall" && p >= 0.37 {
			status = "Paused" // SAB has no "stalled"; a paused job is the closest stuck state
		} else {
			d := *downloadTo
			if j.Mode == "slow" {
				d *= 4
			}
			rest := time.Duration(float64(d) * (1 - p))
			timeleft = fmt.Sprintf("%d:%02d:%02d", int(rest.Hours()), int(rest.Minutes())%60, int(rest.Seconds())%60)
		}
		slots = append(slots, map[string]any{
			"nzo_id": j.ID, "filename": j.Name, "cat": j.Category, "status": status, "priority": "Normal",
			"mb": fmt.Sprintf("%.2f", mb), "mbleft": fmt.Sprintf("%.2f", left), "percentage": strconv.Itoa(int(p * 100)),
			"timeleft": timeleft,
		})
	}
	if slots == nil {
		slots = []map[string]any{}
	}
	return map[string]any{"paused": false, "speed": "10 M", "kbpersec": "10000", "slots": slots, "noofslots": len(slots), "status": "Downloading"}
}

// assignSlots gives waiting jobs a download slot as running ones finish (mu held).
func assignSlots(now time.Time) {
	running := 0
	for _, j := range jobs {
		if !j.Done && !j.Deleted && !j.Started.IsZero() {
			running++
		}
	}
	for _, j := range jobs {
		if running >= *slots {
			return
		}
		if !j.Done && !j.Deleted && j.Started.IsZero() {
			j.Started = now
			running++
		}
	}
}

func history(cat string) map[string]any {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	assignSlots(now)
	var slots []map[string]any
	for _, j := range jobs {
		finish(j, now)
		if !j.Done || j.Deleted || (cat != "" && cat != "*" && j.Category != cat) {
			continue
		}
		st := "Completed"
		if j.Failed != "" {
			st = "Failed"
		}
		slots = append(slots, map[string]any{
			"nzo_id": j.ID, "name": j.Name, "nzb_name": j.Name + ".nzb", "category": j.Category, "status": st,
			"fail_message": j.Failed, "bytes": j.Size, "download_time": 60, "storage": reportPath(j.Name),
			"completed": now.Unix(),
		})
	}
	if slots == nil {
		slots = []map[string]any{}
	}
	return map[string]any{"slots": slots, "noofslots": len(slots)}
}

// ---- control ----

func control(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/sim/mode":
		v := r.URL.Query().Get("value")
		switch v {
		case "ok", "slow", "stall", "fail", "badimport", "none":
			mu.Lock()
			mode = v
			mu.Unlock()
			log.Printf("mode -> %s", v)
			fmt.Fprintln(w, "mode", v)
		default:
			http.Error(w, "value: ok|slow|stall|fail|badimport|none", 400)
		}
	case "/sim/state":
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"mode": mode, "jobs": jobs})
	}
}

func main() {
	flag.Parse()
	if err := os.MkdirAll(*downloads, 0o777); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/newznab/api", newznab)
	mux.HandleFunc("/sabnzbd/api", sab)
	mux.HandleFunc("/api", sab)
	mux.HandleFunc("/sim/", control)
	log.Printf("arrsim on %s: indexer /newznab/api, SABnzbd /sabnzbd (key arrsim), control /sim/mode", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
