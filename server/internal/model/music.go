package model

import "time"

// MusicFolder is a folder the server reads music from.
type MusicFolder struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"createdAt"`
}

// ArtistRef names an artist a track credits.
type ArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MusicArtist is an artist as the lists show one. The counts are of files that are there.
type MusicArtist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Art        string `json:"art,omitempty"` // content hash for /v1/music/art/{hash}
	TrackCount int    `json:"trackCount"`
	AlbumCount int    `json:"albumCount"`
	Favorite   bool   `json:"favorite"`
}

// MusicAlbum is an album: what the tracks that name it add up to.
type MusicAlbum struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	ArtistID   string    `json:"artistId"`
	Artist     string    `json:"artist"`
	Year       int       `json:"year,omitempty"`
	Art        string    `json:"art,omitempty"`
	TrackCount int       `json:"trackCount"`
	DurationMs int64     `json:"durationMs"`
	AddedAt    time.Time `json:"addedAt"`
	Favorite   bool      `json:"favorite"`
}

// MusicTrack is one song: one audio file.
type MusicTrack struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Artist      string      `json:"artist"` // as tagged
	ArtistID    string      `json:"artistId"`
	Artists     []ArtistRef `json:"artists,omitempty"` // everyone credited, the main artist first
	AlbumID     string      `json:"albumId,omitempty"`
	Album       string      `json:"album,omitempty"`
	AlbumArtist string      `json:"albumArtist,omitempty"`
	Disc        int         `json:"disc,omitempty"`
	Track       int         `json:"track,omitempty"`
	Year        int         `json:"year,omitempty"`
	DurationMs  int64       `json:"durationMs"`
	Container   string      `json:"container"`
	Codec       string      `json:"codec"`
	Bitrate     int         `json:"bitrate,omitempty"`
	SampleRate  int         `json:"sampleRate,omitempty"`
	BitDepth    int         `json:"bitDepth,omitempty"`
	Channels    int         `json:"channels,omitempty"`
	Lossless    bool        `json:"lossless"`
	Size        int64       `json:"size"`
	// Art is the picture to show: the file's own, or else its album's.
	Art string `json:"art,omitempty"`
	// Lyrics says where lyrics come from ("lrc", "txt", "embedded"); empty when there are none.
	Lyrics string `json:"lyrics,omitempty"`
	// ReplayGain as the file is tagged, in dB (nil when it has none).
	GainTrack *float64 `json:"gainTrack,omitempty"`
	GainAlbum *float64 `json:"gainAlbum,omitempty"`
	// Loudness measured by the server: the song's integrated loudness (LUFS) and true
	// peak (dBTP), and its album's, measured over all the album's songs as one. Nil until
	// measured (an album's: until every song of it is).
	Loudness      *float64   `json:"loudness,omitempty"`
	Peak          *float64   `json:"peak,omitempty"`
	AlbumLoudness *float64   `json:"albumLoudness,omitempty"`
	AlbumPeak     *float64   `json:"albumPeak,omitempty"`
	Favorite      bool       `json:"favorite"`
	PlayCount     int        `json:"playCount"`
	LastPlayedAt  *time.Time `json:"lastPlayedAt,omitempty"`
	AddedAt       time.Time  `json:"addedAt"`

	Path       string `json:"-"`
	Mtime      int64  `json:"-"`
	LyricsText string `json:"-"`
}

// LosslessCodec reports whether an audio codec keeps every sample of what was recorded.
func LosslessCodec(codec string) bool {
	switch codec {
	case "flac", "alac", "ape", "wavpack", "tta", "truehd", "mlp", "wmalossless", "tak", "shorten":
		return true
	}
	// Plain PCM (wav, aiff) and DSD.
	return len(codec) > 4 && (codec[:4] == "pcm_" || codec[:4] == "dsd_")
}

// MusicPlaylist is a profile's list of songs.
type MusicPlaylist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TrackCount int    `json:"trackCount"` // songs whose files are there
	DurationMs int64  `json:"durationMs"`
	// Art is the pictures of its first songs (up to four, one of each), for a cover.
	Art       []string  `json:"art"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PlaylistEntry is one place in a playlist: a song, and the entry's own id (a song can
// be in a list twice).
type PlaylistEntry struct {
	EntryID int64 `json:"entryId"`
	MusicTrack
}
