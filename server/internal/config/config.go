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
	Listen      string   // ROXY_LISTEN, default :8096
	DataDir     string   // ROXY_DATA_DIR, default ./data (db, caches)
	MediaRoots  []string // ROXY_MEDIA_ROOTS, comma separated; each becomes a library
	FFmpegPath  string   // ROXY_FFMPEG_PATH, default: search PATH then ~/tools/jellyfin-ffmpeg
	FFprobePath string   // ROXY_FFPROBE_PATH
	FontsDir    string   // ROXY_FONTS_DIR, extra fonts for burn-in / client fallback
	ScanOnStart bool     // ROXY_SCAN_ON_START, default true
	Debug       bool     // ROXY_DEBUG
	TMDBAPIKey  string   // ROXY_TMDB_API_KEY; empty disables the TMDB provider (AniList still runs)
	// First-run admin account (only used when the users table is empty).
	AdminUser     string // ROXY_ADMIN_USER, default roxy
	AdminPassword string // ROXY_ADMIN_PASSWORD, default roxy (dev only; set it in production)
}

func Load() (*Config, error) {
	c := &Config{
		Listen:      env("ROXY_LISTEN", ":8096"),
		DataDir:     env("ROXY_DATA_DIR", "data"),
		FFmpegPath:  env("ROXY_FFMPEG_PATH", ""),
		FFprobePath: env("ROXY_FFPROBE_PATH", ""),
		FontsDir:    env("ROXY_FONTS_DIR", ""),
		ScanOnStart: envBool("ROXY_SCAN_ON_START", true),
		Debug:       envBool("ROXY_DEBUG", false),
		TMDBAPIKey:  env("ROXY_TMDB_API_KEY", ""),
		AdminUser:     env("ROXY_ADMIN_USER", "roxy"),
		AdminPassword: env("ROXY_ADMIN_PASSWORD", "roxy"),
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
	return c, nil
}

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
