package library

import (
	"os"
	"path/filepath"
	"strings"
)

// What the scanner leaves alone inside a media folder, and why. The folder browser shows the
// same reasons, so what a user sees there is what a scan will find. Modelled on the folders
// Jellyfin, Plex and Kodi skip, plus download-client folders that sit next to a library.

// skipDirs: folder name (lower case) -> reason.
var skipDirs = map[string]string{
	// Download clients: unfinished or not yet imported by Sonarr/Radarr.
	"downloads":    "Download client folder",
	"download":     "Download client folder",
	"torrents":     "Download client folder",
	"incomplete":   "Unfinished downloads",
	"intermediate": "Unfinished downloads",
	// Extras that belong to a title, not titles of their own.
	"extras":            "Extras",
	"featurettes":       "Extras",
	"behind the scenes": "Extras",
	"deleted scenes":    "Extras",
	"interviews":        "Extras",
	"scenes":            "Extras",
	"shorts":            "Extras",
	"trailers":          "Extras",
	"clips":             "Extras",
	"sample":            "Samples",
	"samples":           "Samples",
	// NAS and OS housekeeping.
	"@eadir":                    "System folder",
	"#recycle":                  "Recycle bin",
	"#snapshot":                 "System folder",
	"$recycle.bin":              "Recycle bin",
	"system volume information": "System folder",
	"lost+found":                "System folder",
}

// IgnoreMarkers: a folder holding one of these files is skipped with everything in it
// (".ignore" is what Jellyfin uses, so existing setups carry over).
var IgnoreMarkers = []string{".ignore", ".roxyignore"}

// SkipDirReason says why the folder at path is skipped, or "" to scan it.
func SkipDirReason(path string) string {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return "Hidden"
	}
	lower := strings.ToLower(name)
	if r, ok := skipDirs[lower]; ok {
		return r
	}
	// SABnzbd and NZBGet leave these while unpacking or after a failure.
	if strings.HasPrefix(lower, "_unpack_") || strings.HasPrefix(lower, "_failed_") {
		return "Unfinished downloads"
	}
	for _, m := range IgnoreMarkers {
		if _, err := os.Stat(filepath.Join(path, m)); err == nil {
			return "Ignored (" + m + " file)"
		}
	}
	return ""
}

// SkipFileReason says why a video file is skipped, or "" to scan it. Only exact sample
// names count, so a show called "Shomin Sample" is still found.
func SkipFileReason(name string) string {
	stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	if strings.HasPrefix(stem, ".") {
		return "Hidden"
	}
	if stem == "sample" || strings.HasPrefix(stem, "sample.") || strings.HasPrefix(stem, "sample-") {
		return "Sample"
	}
	for _, suf := range []string{".sample", "-sample", "_sample"} {
		if strings.HasSuffix(stem, suf) {
			return "Sample"
		}
	}
	return ""
}
