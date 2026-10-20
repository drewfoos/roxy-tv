// Package model holds Roxy's domain types. They mirror the schema in
// server/migrations and the data model in docs/ARCHITECTURE.md section 4.2.
package model

import "time"

type ItemType string

const (
	TypeMovie   ItemType = "movie"
	TypeSeries  ItemType = "series"
	TypeSeason  ItemType = "season"
	TypeEpisode ItemType = "episode"
)

type LibraryKind string

const (
	KindMovies LibraryKind = "movies"
	KindSeries LibraryKind = "series"
)

type Library struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Kind      LibraryKind `json:"kind"`
	Root      string      `json:"root"`
	AnimeMode bool        `json:"animeMode"`
}

type Item struct {
	ID            string            `json:"id"`
	GUID          string            `json:"guid"`
	LibraryID     int64             `json:"libraryId"`
	Type          ItemType          `json:"type"`
	ParentID      *string           `json:"parentId,omitempty"`
	Title         string            `json:"title"`
	SortTitle     string            `json:"sortTitle,omitempty"`
	Year          *int              `json:"year,omitempty"`
	Overview      string            `json:"overview,omitempty"`
	IndexAired    *int              `json:"indexAired,omitempty"`
	SeasonAired   *int              `json:"seasonAired,omitempty"`
	IndexAbsolute *int              `json:"indexAbsolute,omitempty"`
	RuntimeMs     *int64            `json:"runtimeMs,omitempty"`
	ProviderIDs   map[string]string `json:"providerIds,omitempty"`
	DateCreated   time.Time         `json:"dateCreated"`
	DateModified  time.Time         `json:"dateModified"`

	// Provider metadata (docs/api/native.md "Metadata and images"). Empty until the
	// metadata service matched the item.
	Tagline             string     `json:"tagline,omitempty"`
	Genres              []string   `json:"genres,omitempty"`
	Advisories          []string   `json:"advisories,omitempty"`      // "Sexual Content", "Violence", ... (series / movies)
	CommunityRating     *float64   `json:"communityRating,omitempty"` // 0..10
	OfficialRating      string     `json:"officialRating,omitempty"`
	Premiered           string     `json:"premiered,omitempty"` // YYYY-MM-DD
	AirDate             string     `json:"airDate,omitempty"`   // episodes: same value as Premiered
	Status              string     `json:"status,omitempty"`    // series: Continuing | Ended
	Studio              string     `json:"studio,omitempty"`
	SeasonCount         *int       `json:"seasonCount,omitempty"`  // series: seasons the provider knows, specials excluded
	RelatedCount        int        `json:"relatedCount,omitempty"` // series: linked movies / OVAs / side stories
	MetadataRefreshedAt *time.Time `json:"metadataRefreshedAt,omitempty"`
	// Images maps an image type (poster, backdrop, logo, thumb) to its relative URL
	// (/v1/items/{id}/images/{type}). Absent keys mean "no image".
	Images map[string]string `json:"images,omitempty"`

	// IsVirtual marks a placeholder created from provider metadata (a missing or unaired
	// episode, or a season the library has no files for). HasMedia says whether any media
	// source exists; Availability is derived by the API: "available" | "missing" | "unaired".
	IsVirtual    bool   `json:"isVirtual,omitempty"`
	HasMedia     bool   `json:"hasMedia"`
	Availability string `json:"availability,omitempty"`

	// Denormalized for list views (episodes only); resolved by the API layer, not stored.
	SeriesID    string `json:"seriesId,omitempty"`
	SeriesTitle string `json:"seriesTitle,omitempty"`

	// Per-user watch state, attached by the API layer when a user is authenticated.
	UserData *UserData `json:"userData,omitempty"`

	// Populated on detail responses.
	Sources []MediaSource `json:"mediaSources,omitempty"`

	// AudioLanguages are the distinct audio-track languages across a series' episodes or
	// a movie's sources (ISO 639-2), so cards can say "Dub | Sub" without the streams.
	AudioLanguages []string `json:"audioLanguages,omitempty"`
	// IsAnime: the title (or an episode's series) was matched on AniList, so clients use
	// the Dub / Sub vocabulary instead of language names.
	IsAnime bool `json:"isAnime,omitempty"`
}

// UserData is the per-user watch state as the native API renders it.
type UserData struct {
	PositionMs    int64      `json:"positionMs"`
	Played        bool       `json:"played"`
	PlayCount     int        `json:"playCount"`
	IsFavorite    bool       `json:"isFavorite"`
	LastPlayedAt  *time.Time `json:"lastPlayedAt,omitempty"`
	AudioIndex    *int       `json:"audioIndex,omitempty"`
	SubtitleIndex *int       `json:"subtitleIndex,omitempty"`
}

// ImageTypes are the image kinds an item can carry. "hero" is a textless poster (no
// title baked in) for the phone's Home banner, where the logo is drawn over it.
var ImageTypes = []string{"poster", "backdrop", "logo", "thumb", "hero"}

func IsImageType(t string) bool {
	for _, x := range ImageTypes {
		if x == t {
			return true
		}
	}
	return false
}

type MediaSource struct {
	ID           string        `json:"id"`
	ItemID       string        `json:"itemId"`
	Name         string        `json:"name,omitempty"`
	Container    string        `json:"container,omitempty"`
	Size         int64         `json:"size"`
	DurationMs   int64         `json:"durationMs"`
	Bitrate      int64         `json:"bitrate,omitempty"`
	VersionLabel string        `json:"versionLabel,omitempty"`
	IsPrimary    bool          `json:"isPrimary"`
	Parts        []MediaPart   `json:"parts,omitempty"`
	Streams      []MediaStream `json:"streams,omitempty"`
	Attachments  []Attachment  `json:"attachments,omitempty"`
	// Segments are skippable ranges (intro, outro, recap, preview) in milliseconds.
	Segments []MediaSegment `json:"segments,omitempty"`
}

type MediaSegment struct {
	Type    string `json:"type"` // intro | outro | recap | preview
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Origin  string `json:"origin,omitempty"` // chapters | fingerprint | manual
}

type MediaPart struct {
	ID         string    `json:"id"`
	SourceID   string    `json:"sourceId"`
	Index      int       `json:"index"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
	DurationMs int64     `json:"durationMs,omitempty"`
}

type StreamType string

const (
	StreamVideo    StreamType = "video"
	StreamAudio    StreamType = "audio"
	StreamSubtitle StreamType = "subtitle"
)

type RangeType string

const (
	RangeSDR       RangeType = "SDR"
	RangeHDR10     RangeType = "HDR10"
	RangeHDR10Plus RangeType = "HDR10+"
	RangeHLG       RangeType = "HLG"
	RangeDV        RangeType = "DV"
)

type MediaStream struct {
	SourceID       string     `json:"sourceId"`
	Index          int        `json:"index"`
	Type           StreamType `json:"type"`
	Codec          string     `json:"codec"`
	Profile        string     `json:"profile,omitempty"`
	Level          int        `json:"level,omitempty"`
	BitDepth       int        `json:"bitDepth,omitempty"`
	PixFmt         string     `json:"pixFmt,omitempty"`
	Width          int        `json:"width,omitempty"`
	Height         int        `json:"height,omitempty"`
	FPS            float64    `json:"fps,omitempty"`
	ColorPrimaries string     `json:"colorPrimaries,omitempty"`
	ColorTransfer  string     `json:"colorTransfer,omitempty"`
	ColorSpace     string     `json:"colorSpace,omitempty"`
	RangeType      RangeType  `json:"rangeType,omitempty"`
	DVProfile      int        `json:"dvProfile,omitempty"`
	Channels       int        `json:"channels,omitempty"`
	ChannelLayout  string     `json:"channelLayout,omitempty"`
	SampleRate     int        `json:"sampleRate,omitempty"`
	Bitrate        int64      `json:"bitrate,omitempty"`
	Language       string     `json:"language,omitempty"`
	// DetectedLanguage is sniffed from the text of subtitle tracks at ingest. Clients
	// should prefer it over Language when both exist: tags lie, text does not.
	DetectedLanguage  string `json:"detectedLanguage,omitempty"`
	Title             string `json:"title,omitempty"`
	IsDefault         bool   `json:"isDefault"`
	IsForced          bool   `json:"isForced"`
	IsHearingImpaired bool   `json:"isHearingImpaired"`
	IsExternal        bool   `json:"isExternal"`
	ExternalPath      string `json:"externalPath,omitempty"`
	IsTextFormat      bool   `json:"isTextFormat"`
}

type Attachment struct {
	SourceID      string     `json:"sourceId"`
	Index         int        `json:"index"`
	Filename      string     `json:"filename"`
	Mime          string     `json:"mime,omitempty"`
	ExtractedPath string     `json:"-"`
	ExtractedAt   *time.Time `json:"extractedAt,omitempty"`
}

// IsTextSubtitleCodec is the single classifier used everywhere (Jellyfin's IsTextFormat).
func IsTextSubtitleCodec(codec string) bool {
	switch codec {
	case "ass", "ssa", "subrip", "srt", "webvtt", "vtt", "mov_text", "text", "microdvd", "sami":
		return true
	}
	return false
}
