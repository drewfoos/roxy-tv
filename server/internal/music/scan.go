// Package music reads music folders into the library: every audio file is a song, named
// by its tags, and albums and artists are what the songs add up to.
package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

// missingKept is how long the row of a file that is gone is kept (hidden), so that the
// file coming back, or the song coming back as another file, has its history.
var missingKept = 30 * 24 * time.Hour

// Service scans the music folders and keeps the cover pictures.
type Service struct {
	store *store.Store
	ff    *ffmpeg.FFmpeg
	art   *ArtStore

	// Workers is how many files are read at once.
	Workers int

	mu        sync.Mutex
	scanning  bool
	measuring bool // a loudness pass is running (loudness.go)
	again     bool // a scan was asked for while one ran
	againAll  bool // ... and it was a forced one
	status    Status

	picMu sync.Mutex
	pics  map[string]picture // folder pictures already read, by path
}

type picture struct {
	size  int64
	mtime int64
	hash  string
}

// Status says what the scanner is doing and what its last pass found.
type Status struct {
	Scanning   bool           `json:"scanning"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Done       int            `json:"done"`  // files read so far in this pass
	Total      int            `json:"total"` // files this pass has to read
	Folders    []FolderResult `json:"folders"`
	Error      string         `json:"error,omitempty"`
}

// FolderResult is what one pass did in one folder.
type FolderResult struct {
	FolderID int64  `json:"folderId"`
	Name     string `json:"name"`
	Files    int    `json:"files"`   // audio files found
	Added    int    `json:"added"`   // new songs
	Updated  int    `json:"updated"` // files read again because they changed
	Moved    int    `json:"moved"`   // songs found at a new place (or as a new file)
	Missing  int    `json:"missing"` // songs whose file is gone (hidden, kept)
	Removed  int    `json:"removed"` // rows of files long gone, dropped
	Failed   int    `json:"failed"`  // files that could not be read
	// LeftAlone says why nothing was changed, when the folder could not be read or held
	// no music at all (which is what an unmounted drive looks like).
	LeftAlone string   `json:"leftAlone,omitempty"`
	Problems  []string `json:"problems,omitempty"` // the first few files that failed, and why
}

// New makes the scanner. Pictures are kept under artDir (inside the data folder).
func New(st *store.Store, ff *ffmpeg.FFmpeg, artDir string) *Service {
	return &Service{store: st, ff: ff, art: NewArtStore(artDir), Workers: 4, pics: map[string]picture{}}
}

// EnsureFolders makes a music folder of every configured root (ROXY_MUSIC_ROOTS) that is
// not one yet. A root an admin removed in the app is not brought back by the setting.
func EnsureFolders(ctx context.Context, st *store.Store, roots []string) error {
	if len(roots) == 0 {
		return nil
	}
	removed, err := st.RemovedRoots(ctx)
	if err != nil {
		return err
	}
	folders, err := st.MusicFolders(ctx)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, f := range folders {
		have[f.Root] = true
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if have[root] || removed[root] {
			continue
		}
		f := model.MusicFolder{Name: filepath.Base(root), Root: root}
		if err := st.CreateMusicFolder(ctx, &f); err != nil {
			return err
		}
		have[root] = true
		slog.Info("music folder added from ROXY_MUSIC_ROOTS", "name", f.Name, "root", root)
	}
	return nil
}

// Art is where the cover pictures are kept.
func (s *Service) Art() *ArtStore { return s.art }

// Status reports the scanner's state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Folders = append([]FolderResult(nil), s.status.Folders...)
	return st
}

// Run scans at once (now) and then every interval (again) until ctx ends: files copied
// in, retagged or removed show up without anyone asking. A pass over a library that has
// not changed only lists the folders.
func (s *Service) Run(ctx context.Context, interval time.Duration, now, again bool) {
	for first := true; ; first = false {
		if !first || now {
			if err := s.Scan(ctx, false); err != nil && ctx.Err() == nil {
				slog.Warn("music: scan failed", "err", err)
			}
			// What the scan brought in (or a changed file) is measured for loudness.
			if _, err := s.MeasureLoudness(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("music: loudness pass failed", "err", err)
			}
		}
		if !again {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Scan reads every music folder. force reads every file again, not only new and changed
// ones. A call while a scan runs returns at once, and one more pass runs when it ends.
func (s *Service) Scan(ctx context.Context, force bool) error {
	s.mu.Lock()
	if s.scanning {
		s.again, s.againAll = true, s.againAll || force
		s.mu.Unlock()
		return nil
	}
	s.scanning = true
	s.mu.Unlock()
	for {
		err := s.scanAll(ctx, force)
		s.mu.Lock()
		if s.again && ctx.Err() == nil {
			force, s.again, s.againAll = s.againAll, false, false
			s.mu.Unlock()
			continue
		}
		s.scanning = false
		s.again, s.againAll = false, false
		s.mu.Unlock()
		return err
	}
}

func (s *Service) scanAll(ctx context.Context, force bool) error {
	started := time.Now()
	s.mu.Lock()
	s.status = Status{Scanning: true, StartedAt: &started}
	s.mu.Unlock()
	folders, err := s.store.MusicFolders(ctx)
	var results []FolderResult
	if err == nil {
		for _, f := range folders {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			res, ferr := s.scanFolder(ctx, f, force)
			results = append(results, res)
			if ferr != nil {
				err = errors.Join(err, fmt.Errorf("%s: %w", f.Name, ferr))
				continue
			}
			if res.LeftAlone != "" {
				slog.Warn("music: folder left as it is", "folder", f.Name, "root", f.Root, "why", res.LeftAlone)
			} else if res.Added+res.Updated+res.Moved+res.Missing+res.Removed+res.Failed > 0 {
				slog.Info("music: scanned", "folder", f.Name, "files", res.Files, "added", res.Added, "updated", res.Updated,
					"moved", res.Moved, "missing", res.Missing, "removed", res.Removed, "failed", res.Failed, "took", time.Since(started).Round(time.Millisecond))
			}
		}
	}
	if err == nil && ctx.Err() == nil {
		// Pictures nothing shows any more go; a failure here loses nothing.
		if keep, kerr := s.store.MusicArtInUse(ctx); kerr == nil {
			if n, serr := s.art.Sweep(keep); serr != nil {
				slog.Warn("music: pictures not cleaned up", "err", serr)
			} else if n > 0 {
				slog.Info("music: removed pictures nothing uses", "files", n)
			}
		}
	}
	finished := time.Now()
	s.mu.Lock()
	s.status = Status{StartedAt: &started, FinishedAt: &finished, Folders: results}
	if err != nil {
		s.status.Error = err.Error()
	}
	s.mu.Unlock()
	return err
}

// diskFile is an audio file as the walk found it.
type diskFile struct {
	path   string
	size   int64
	mtime  int64
	top    string // the folder directly under the music folder it is in ("" for a file at the top)
	lyrics string // lyrics file beside it: "lrc", "txt" or ""
}

// walkMusic lists the audio files under root, and for every folder the names in it
// (lower case), which say what pictures and lyrics files are there.
func walkMusic(ctx context.Context, root string) (files []diskFile, dirNames map[string][]string, err error) {
	dirNames = map[string][]string{}
	var walk func(dir, top string) error
	walk = func(dir, top string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if dir == root {
				return err
			}
			slog.Warn("music: cannot read a folder", "dir", dir, "err", err)
			return nil
		}
		names := make([]string, 0, len(entries))
		present := map[string]bool{}
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
				present[strings.ToLower(e.Name())] = true
			}
		}
		// A folder holding an ignore file is not read, nor anything under it.
		if present[".ignore"] || present[".roxyignore"] || present[".nomedia"] {
			return nil
		}
		dirNames[dir] = names
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || skippedDirs[strings.ToLower(name)] {
				continue
			}
			full := filepath.Join(dir, name)
			if e.IsDir() {
				sub := top
				if dir == root {
					sub = name
				}
				if err := walk(full, sub); err != nil {
					return err
				}
				continue
			}
			// Regular files only: a link could lead out of the music folder.
			if !e.Type().IsRegular() || !IsAudioFile(name) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, diskFile{path: full, size: info.Size(), mtime: info.ModTime().UnixNano(), top: top, lyrics: SidecarKind(name, present)})
		}
		return nil
	}
	err = walk(root, "")
	return files, dirNames, err
}

// skippedDirs are folders that hold no music whatever is in them.
var skippedDirs = map[string]bool{
	"@eadir": true, "#recycle": true, "$recycle.bin": true, "system volume information": true, "lost+found": true,
}

// read is one file read by a worker.
type read struct {
	file diskFile
	info *FileInfo
	art  string
	err  error
}

func (s *Service) scanFolder(ctx context.Context, folder model.MusicFolder, force bool) (FolderResult, error) {
	res := FolderResult{FolderID: folder.ID, Name: folder.Name}
	states, err := s.store.MusicFileStates(ctx, folder.ID)
	if err != nil {
		return res, err
	}
	files, dirNames, err := walkMusic(ctx, folder.Root)
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.LeftAlone = "the folder cannot be read: " + err.Error()
		return res, nil
	}
	res.Files = len(files)
	known := 0
	for _, st := range states {
		if !st.Missing {
			known++
		}
	}
	// No music at all where there was some: a drive that is not mounted looks exactly like
	// this (Docker even makes the empty folder). Nothing is touched; the songs stay listed.
	if len(files) == 0 && known > 0 {
		res.LeftAlone = "no music files were found in it, so its songs were kept (is the drive mounted?)"
		return res, nil
	}

	byPath := make(map[string]store.MusicFileState, len(states))
	for _, st := range states {
		byPath[st.Path] = st
	}
	onDisk := make(map[string]bool, len(files))
	var toRead []diskFile
	for _, f := range files {
		onDisk[f.path] = true
		st, seen := byPath[f.path]
		if !seen || force || st.Missing || st.Size != f.size || st.Mtime != f.mtime {
			toRead = append(toRead, f)
			continue
		}
		// Unchanged: only a lyrics file beside it may have come or gone.
		want := f.lyrics
		if want == "" && st.HasText {
			want = "embedded"
		}
		if want != st.Lyrics {
			if err := s.store.SetMusicTrackLyrics(ctx, st.ID, want); err != nil {
				return res, err
			}
		}
	}

	s.mu.Lock()
	s.status.Total += len(toRead)
	s.mu.Unlock()
	reads := s.readFiles(ctx, toRead)
	if err := ctx.Err(); err != nil {
		return res, err
	}

	// Names the library knows as one artist: its artist folders, the artists it already
	// has, and every artist tag of this pass that names one artist plainly.
	knownNames, err := s.store.MusicArtistNames(ctx)
	if err != nil {
		return res, err
	}
	for _, f := range files {
		if f.top != "" {
			knownNames[Fold(f.top)] = true
		}
	}
	for _, r := range reads {
		if r.err == nil && r.info.Artist != "" && plainName(r.info.Artist) {
			knownNames[Fold(r.info.Artist)] = true
		}
	}
	isKnown := func(folded string) bool { return knownNames[folded] }

	// Rows whose file is not where it was: a new file of the same song takes one over.
	gone := map[string][]store.MusicFileState{}
	for _, st := range states {
		if !onDisk[st.Path] {
			gone[st.Identity] = append(gone[st.Identity], st)
		}
	}
	// The songs that are there, by identity (for a row that leaves while another file of
	// the same song stays).
	here := map[string]string{}
	for _, st := range states {
		if onDisk[st.Path] && !st.Missing {
			here[st.Identity] = st.ID
		}
	}

	now := time.Now()
	for _, r := range reads {
		if r.err != nil {
			res.Failed++
			if len(res.Problems) < 5 {
				res.Problems = append(res.Problems, filepath.Base(r.file.path)+": "+r.err.Error())
			}
			slog.Warn("music: file not read", "file", r.file.path, "err", r.err)
			continue
		}
		row := buildRow(folder.ID, r, isKnown)
		st, seen := byPath[r.file.path]
		switch {
		case seen:
			row.ID = st.ID
			if st.Missing {
				res.Moved++ // back where it was
			} else {
				res.Updated++
			}
		default:
			if old, ok := takeGone(gone, row.Identity, r.file.size, row.DurationMs); ok {
				row.ID = old.ID
				res.Moved++
			} else {
				row.ID = uuid.NewString()
				res.Added++
			}
			// A new song is as new as its file (a library read for the first time is not
			// all "added today"), never newer than now.
			row.AddedAt = time.Unix(0, r.file.mtime)
			if row.AddedAt.After(now) {
				row.AddedAt = now
			}
		}
		if err := s.store.SaveMusicTrack(ctx, row); err != nil {
			return res, fmt.Errorf("saving %s: %w", filepath.Base(r.file.path), err)
		}
		here[row.Identity] = row.ID
	}

	// What is left of `gone` has no file any more. Where another file of the same song is
	// there, it inherits the plays and the favourite mark, and the row goes; otherwise the
	// row is hidden and kept for a while.
	var missing []string
	for identity, list := range gone {
		for _, st := range list {
			if heir, ok := here[identity]; ok && heir != st.ID {
				if err := s.store.MoveMusicUserData(ctx, st.ID, heir); err != nil {
					return res, err
				}
				if err := s.store.DeleteMusicTrack(ctx, st.ID); err != nil {
					return res, err
				}
				res.Moved++
				continue
			}
			if !st.Missing {
				missing = append(missing, st.ID)
			}
		}
	}
	if err := s.store.MarkMusicMissing(ctx, missing); err != nil {
		return res, err
	}
	res.Missing = len(missing)
	if n, err := s.store.PurgeMusicMissing(ctx, folder.ID, now.Add(-missingKept)); err != nil {
		return res, err
	} else {
		res.Removed = n
	}

	albumArt, artistArt, err := s.folderPictures(ctx, folder, dirNames)
	if err != nil {
		return res, err
	}
	if err := s.store.RefreshMusicCatalog(ctx, albumArt, artistArt); err != nil {
		return res, err
	}
	return res, nil
}

// plainName reports whether an artist tag holds nothing that could separate two artists.
func plainName(tag string) bool {
	if len(splitAny(tag, strongSeps)) > 1 {
		return false
	}
	return !strings.Contains(tag, ", ") && !strings.Contains(tag, " & ") && !strings.Contains(strings.ToLower(tag), " x ")
}

// takeGone picks, among the rows of files that left, the one a new file of the same song
// takes over: the same bytes when there is one (a move), else the closest in length.
func takeGone(gone map[string][]store.MusicFileState, identity string, size, durationMs int64) (store.MusicFileState, bool) {
	list := gone[identity]
	if len(list) == 0 {
		return store.MusicFileState{}, false
	}
	best := 0
	for i, st := range list {
		if st.Size == size {
			best = i
			break
		}
		if abs64(st.DurationMs-durationMs) < abs64(list[best].DurationMs-durationMs) {
			best = i
		}
	}
	st := list[best]
	gone[identity] = append(list[:best:best], list[best+1:]...)
	if len(gone[identity]) == 0 {
		delete(gone, identity)
	}
	return st, true
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// readFiles reads the tags and the picture of each file, several at a time.
func (s *Service) readFiles(ctx context.Context, files []diskFile) []read {
	out := make([]read, len(files))
	workers := s.Workers
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				out[i] = s.readOne(ctx, files[i])
				s.mu.Lock()
				s.status.Done++
				s.mu.Unlock()
			}
		}()
	}
	for i := range files {
		if ctx.Err() != nil {
			break
		}
		next <- i
	}
	close(next)
	wg.Wait()
	return out
}

func (s *Service) readOne(ctx context.Context, f diskFile) read {
	r := read{file: f}
	r.info, r.err = ReadFile(ctx, s.ff, f.path)
	if r.err != nil {
		return r
	}
	if r.info.PictureStream >= 0 {
		// A picture that cannot be copied out or is not an image is no picture: the song
		// is still a song.
		if data, err := EmbeddedPicture(ctx, s.ff, f.path, r.info.PictureStream); err == nil {
			if hash, err := s.art.Put(data); err == nil {
				r.art = hash
			}
		}
	}
	return r
}

// Identity says which song a file is: its main artist and its title, folded. Two files
// with the same identity are the same song (an MP3 and the FLAC that replaces it).
func Identity(artist, title string) string { return Fold(artist) + keySep + Fold(title) }

// keySep stands between the parts of a key. Not a NUL: SQLite's LIKE reads text as far as
// the first one, and albums are searched by their key.
const keySep = "\x1f"

// AlbumKey says which album a song is on: the album's artist and its title, folded.
func AlbumKey(albumArtist, title string) string { return Fold(albumArtist) + keySep + Fold(title) }

func artistRow(name string) store.MusicArtistRow {
	return store.MusicArtistRow{ID: ArtistID(name), Key: Fold(name), Name: name, SortName: SortName(name)}
}

// buildRow turns what a file says into its track row (the id is set by the caller).
func buildRow(folderID int64, r read, known func(string) bool) *store.MusicTrackRow {
	info, f := r.info, r.file
	title := info.Title
	if title == "" {
		title = TitleFromName(f.path)
	}
	// The artist tag, or the artist folder for a file that has none.
	artistTag := info.Artist
	if artistTag == "" {
		artistTag = f.top
	}
	var names []string
	if len(info.Artists) > 1 {
		names = orderCredits(info.Artists, artistTag, f.top) // the file names each artist itself
	} else {
		names = Credits(artistTag, f.top, known)
	}
	if artistTag == "" {
		artistTag = names[0]
	}
	credits := make([]store.MusicArtistRow, 0, len(names))
	for _, n := range names {
		credits = append(credits, artistRow(n))
	}

	row := &store.MusicTrackRow{
		FolderID: folderID, Path: f.path, Size: f.size, Mtime: f.mtime,
		Identity: Identity(names[0], title), Title: title, SortTitle: SortName(title),
		Artist: artistTag, Credits: credits,
		Disc: info.Disc, Track: info.Track, Year: info.Year, Genres: info.Genres,
		DurationMs: info.DurationMs, Container: info.Container, Codec: info.Codec, Bitrate: info.Bitrate,
		SampleRate: info.SampleRate, BitDepth: info.BitDepth, Channels: info.Channels,
		Art: r.art, LyricsText: info.Lyrics, MBRecording: info.MBRecording, MBAlbum: info.MBAlbum,
		GainTrack: info.GainTrack, PeakTrack: info.PeakTrack, GainAlbum: info.GainAlbum, PeakAlbum: info.PeakAlbum,
	}
	row.Lyrics = f.lyrics
	if row.Lyrics == "" && info.Lyrics != "" {
		row.Lyrics = "embedded"
	}
	search := []string{title, artistTag, info.Album}
	search = append(search, names...)
	row.Search = Fold(strings.Join(search, " "))

	if info.Album != "" {
		albumArtist := names[0]
		switch {
		case info.Compilation:
			albumArtist = VariousArtists
		case info.AlbumArtist != "":
			albumArtist = Credits(info.AlbumArtist, f.top, known)[0]
			if va := Fold(albumArtist); va == "various artists" || va == "various" || va == "va" {
				albumArtist = VariousArtists
			}
		}
		key := AlbumKey(albumArtist, info.Album)
		row.Album = &store.MusicAlbumRow{ID: keyID("album", key), Key: key, Title: info.Album, SortTitle: SortName(info.Album), Artist: artistRow(albumArtist)}
		row.AlbumArtist = albumArtist
	}
	return row
}

// folderPictures finds the pictures kept in the folders: an album's ("cover.jpg" in a
// folder that holds that album and nothing else) and an artist's ("artist.jpg" in a top
// folder all of whose songs that artist leads). It also gives each artist the spelling of
// their folder ("Bring Me the Horizon"), whatever the tags of single files say.
func (s *Service) folderPictures(ctx context.Context, folder model.MusicFolder, dirNames map[string][]string) (albumArt, artistArt map[string]string, err error) {
	tracks, _, err := s.store.ListMusicTracks(ctx, "", store.MusicTrackQuery{})
	if err != nil {
		return nil, nil, err
	}
	albumsIn := map[string]map[string]bool{}  // folder -> the albums of the songs directly in it
	albumDirs := map[string]map[string]bool{} // album -> the folders its songs are in
	leaders := map[string]map[string]bool{}   // top folder -> the artists leading its songs
	for _, t := range tracks {
		if !within(t.Path, folder.Root) {
			continue
		}
		dir := filepath.Dir(t.Path)
		if albumsIn[dir] == nil {
			albumsIn[dir] = map[string]bool{}
		}
		albumsIn[dir][t.AlbumID] = true
		if t.AlbumID != "" {
			if albumDirs[t.AlbumID] == nil {
				albumDirs[t.AlbumID] = map[string]bool{}
			}
			albumDirs[t.AlbumID][dir] = true
		}
		if top := topDir(folder.Root, t.Path); top != "" {
			if leaders[top] == nil {
				leaders[top] = map[string]bool{}
			}
			leaders[top][t.ArtistID] = true
		}
	}
	albumArt, artistArt = map[string]string{}, map[string]string{}
	for album, dirs := range albumDirs {
		if len(dirs) != 1 {
			continue
		}
		for dir := range dirs {
			// The folder is the album's own: nothing else is in it, and it is not an
			// artist's folder with loose songs (whose picture would be the artist's).
			if len(albumsIn[dir]) != 1 || filepath.Dir(dir) == filepath.Clean(folder.Root) || dir == filepath.Clean(folder.Root) {
				continue
			}
			if name := pickFolderArt(dirNames[dir]); name != "" {
				if hash := s.pictureHash(filepath.Join(dir, name)); hash != "" {
					albumArt[album] = hash
				}
			}
		}
	}
	tops := make([]string, 0, len(leaders))
	for top := range leaders {
		tops = append(tops, top)
	}
	sort.Strings(tops)
	for _, top := range tops {
		if len(leaders[top]) != 1 {
			continue
		}
		dir := filepath.Join(folder.Root, top)
		for artistID := range leaders[top] {
			if artistID == ArtistID(top) && plainSpelling(top) {
				if err := s.store.RenameMusicArtist(ctx, artistID, top, SortName(top)); err != nil {
					return nil, nil, err
				}
			}
			if name := pickFolderArt(dirNames[dir], "artist"); name != "" {
				if hash := s.pictureHash(filepath.Join(dir, name)); hash != "" {
					artistArt[artistID] = hash
				}
			}
		}
	}
	return albumArt, artistArt, nil
}

// plainSpelling reports whether a folder's name can be how its artist is spelled. Tools
// that make folders swap the characters a file name may not hold for look-alikes ("？",
// "：", "，", "_"): such a name is the folder's, not the artist's.
func plainSpelling(name string) bool {
	for _, r := range name {
		if r == '_' || (r >= 0xFF00 && r <= 0xFFEF) || r == '⧸' || r == '∕' || r == '꞉' {
			return false
		}
	}
	return true
}

// pictureHash keeps a picture file from a music folder and returns its hash ("" when it
// is not a picture). A file that has not changed since it was last read is not read again.
func (s *Service) pictureHash(path string) string {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	s.picMu.Lock()
	if p, ok := s.pics[path]; ok && p.size == fi.Size() && p.mtime == fi.ModTime().UnixNano() && (p.hash == "" || s.art.Has(p.hash)) {
		s.picMu.Unlock()
		return p.hash
	}
	s.picMu.Unlock()
	hash := ""
	if data, err := readPicture(path); err == nil {
		hash, _ = s.art.Put(data)
	}
	s.picMu.Lock()
	s.pics[path] = picture{size: fi.Size(), mtime: fi.ModTime().UnixNano(), hash: hash}
	s.picMu.Unlock()
	return hash
}

// topDir is the name of the folder directly under root that path is in ("" for a file
// at the top).
func topDir(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// within reports whether path is below root.
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
