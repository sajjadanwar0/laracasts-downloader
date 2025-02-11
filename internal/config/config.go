// config.go

package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Required environment variables
var RequiredEnvVars = []string{
	"EMAIL",
	"PASSWORD",
	"DOWNLOAD_PATH", // Now required
	"VIDEO_QUALITY", // Now required
}

// Laracasts base URLs and paths
const (
	LaracastsBaseUrl       = "https://laracasts.com"
	LaracastsPostLoginPath = "/sessions"
	LaracastsSeriesPath    = "/series"
	LaracastsWatchPath     = "/watch/series"
	LaracastsBitsPath      = "/bits"
	LaracastsTopicsPath    = "/topics"
	LaracastsBrowsePath    = "/browse"
)

// DefaultHeaders HTTP request headers
var DefaultHeaders = map[string]string{
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"Accept-Language": "en-US,en;q=0.9",
	"Connection":      "keep-alive",
	"Cache-Control":   "no-cache",
}

var JsonHeaders = map[string]string{
	"Accept":           "application/json",
	"X-Requested-With": "XMLHttpRequest",
	"Content-Type":     "application/json",
}

// GetDownloadPath returns the processed download path from env
func GetDownloadPath() string {
	path := os.Getenv("DOWNLOAD_PATH")

	// Expand ~ to home directory if present
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	return path
}

// GetVideoQuality returns the video quality from env
func GetVideoQuality() string {
	return os.Getenv("VIDEO_QUALITY")
}

// ValidateVideoQuality checks if the provided quality is valid
func ValidateVideoQuality(quality string) bool {
	validQualities := map[string]bool{
		"360p":  true,
		"540p":  true,
		"720p":  true,
		"1080p": true,
	}
	return validQualities[quality]
}
