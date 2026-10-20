// Package config loads Roxy Server settings from environment variables.
//
// Every setting has a ROXY_ prefix. A roxy.yaml file is planned; env is enough for now
// and is what the Docker image uses.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Listen      string // ROXY_LISTEN, default :8096
	HWAccel     string // ROXY_HWACCEL: auto | nvenc | qsv | vaapi | amf | none (auto: the first that works, else software x264)
	VAAPIDevice string // ROXY_VAAPI_DEVICE, default /dev/dri/renderD128
	// LAN discovery: the UDP port the beacon answers "who is roxy?" on (same number as
	// the HTTP default so there is one port to remember and publish); 0 turns it off.
	DiscoveryPort int      // ROXY_DISCOVERY_PORT, default 8096
	AdvertiseURL  string   // ROXY_ADVERTISE_URL: the URL clients should use when the LAN address is wrong (Docker bridge, proxy)
	DataDir       string   // ROXY_DATA_DIR, default ./data (db, caches)
	MediaRoots    []string // ROXY_MEDIA_ROOTS, comma separated; each becomes a library
	FFmpegPath    string   // ROXY_FFMPEG_PATH, default: search PATH then ~/tools/jellyfin-ffmpeg
	FFprobePath   string   // ROXY_FFPROBE_PATH
	FontsDir      string   // ROXY_FONTS_DIR, extra fonts for burn-in / client fallback
	ScanOnStart   bool     // ROXY_SCAN_ON_START, default true
	Debug         bool     // ROXY_DEBUG
	TMDBAPIKey    string   // ROXY_TMDB_API_KEY; empty disables the TMDB provider (AniList still runs)
	// Discover requests are forwarded to Sonarr / Radarr when these are set; without
	// them requests are recorded and shown as pending.
	SonarrURL            string // ROXY_SONARR_URL, e.g. http://sonarr:8989
	SonarrAPIKey         string // ROXY_SONARR_API_KEY
	SonarrRootFolder     string // ROXY_SONARR_ROOT_FOLDER (default: the first one Sonarr reports)
	SonarrQualityProfile string // ROXY_SONARR_QUALITY_PROFILE: id or name (default: the first)
	RadarrURL            string // ROXY_RADARR_URL
	RadarrAPIKey         string // ROXY_RADARR_API_KEY
	RadarrRootFolder     string // ROXY_RADARR_ROOT_FOLDER
	RadarrQualityProfile string // ROXY_RADARR_QUALITY_PROFILE
	// First-run admin account (only used when the users table is empty).
	AdminUser     string // ROXY_ADMIN_USER, default roxy
	AdminPassword string // ROXY_ADMIN_PASSWORD, default roxy (dev only; set it in production)
}

func Load() (*Config, error) {
	c := &Config{
		Listen:               env("ROXY_LISTEN", ":8096"),
		HWAccel:              env("ROXY_HWACCEL", "auto"),
		VAAPIDevice:          env("ROXY_VAAPI_DEVICE", ""),
		DiscoveryPort:        envInt("ROXY_DISCOVERY_PORT", 8096),
		AdvertiseURL:         strings.TrimRight(env("ROXY_ADVERTISE_URL", ""), "/"),
		DataDir:              env("ROXY_DATA_DIR", "data"),
		FFmpegPath:           env("ROXY_FFMPEG_PATH", ""),
		FFprobePath:          env("ROXY_FFPROBE_PATH", ""),
		FontsDir:             env("ROXY_FONTS_DIR", ""),
		ScanOnStart:          envBool("ROXY_SCAN_ON_START", true),
		Debug:                envBool("ROXY_DEBUG", false),
		TMDBAPIKey:           env("ROXY_TMDB_API_KEY", ""),
		SonarrURL:            strings.TrimRight(env("ROXY_SONARR_URL", ""), "/"),
		SonarrAPIKey:         env("ROXY_SONARR_API_KEY", ""),
		SonarrRootFolder:     env("ROXY_SONARR_ROOT_FOLDER", ""),
		SonarrQualityProfile: env("ROXY_SONARR_QUALITY_PROFILE", ""),
		RadarrURL:            strings.TrimRight(env("ROXY_RADARR_URL", ""), "/"),
		RadarrAPIKey:         env("ROXY_RADARR_API_KEY", ""),
		RadarrRootFolder:     env("ROXY_RADARR_ROOT_FOLDER", ""),
		RadarrQualityProfile: env("ROXY_RADARR_QUALITY_PROFILE", ""),
		AdminUser:            env("ROXY_ADMIN_USER", "roxy"),
		AdminPassword:        env("ROXY_ADMIN_PASSWORD", "roxy"),
	}
	for _, r := range strings.Split(env("ROXY_MEDIA_ROOTS", ""), ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		c.MediaRoots = append(c.MediaRoots, abs)
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, err
	}
	c.DataDir = abs
	// TMDB key resolution, in order: env, <DataDir>/tmdb.key (git-ignored, lives in the
	// Docker volume), then a project default compiled in by CI (see DefaultTMDBAPIKey).
	// Keys never live in the source tree.
	if c.TMDBAPIKey == "" {
		if b, err := os.ReadFile(filepath.Join(c.DataDir, "tmdb.key")); err == nil {
			c.TMDBAPIKey = strings.TrimSpace(string(b))
		}
	}
	if c.TMDBAPIKey == "" {
		c.TMDBAPIKey = DefaultTMDBAPIKey
	}
	return c, nil
}

// DefaultTMDBAPIKey is the project key for release builds, injected by the build:
//
//	go build -ldflags "-X github.com/roxy-media/roxy/server/internal/config.DefaultTMDBAPIKey=<key>"
//
// It is empty in source so the repository never carries a credential (the CI secret
// supplies it), which is how Jellyfin-style "no key needed" behaviour is achieved.
var DefaultTMDBAPIKey = ""

func (c *Config) DBPath() string         { return filepath.Join(c.DataDir, "roxy.db") }
func (c *Config) CacheDir() string       { return filepath.Join(c.DataDir, "cache") }
func (c *Config) AttachmentsDir() string { return filepath.Join(c.CacheDir(), "attachments") }
func (c *Config) SubtitlesDir() string   { return filepath.Join(c.CacheDir(), "subtitles") }
func (c *Config) ImagesDir() string      { return filepath.Join(c.CacheDir(), "images") }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Version is reported by system/info and the discovery beacon.
const Version = "0.1.0-dev"

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
