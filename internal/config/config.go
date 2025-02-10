// internal/config/config.go

package config

// Lara casts base URLs and paths
const (
	LaracastsBaseUrl       = "https://laracasts.com"
	LaracastsPostLoginPath = "/sessions"
	LaracastsSeriesPath    = "/series"
	LaracastsApiPath       = "/api/series"
	LaracastsBitsPath      = "/bits"
	LaracastsTopicsPath    = "/topics"
	LaracastsBrowsePath    = "/browse"
)

// DefaultHeaders HTTP request headers
var DefaultHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

var InertiaHeaders = map[string]string{
	"X-Inertia":         "true",
	"X-Inertia-Version": "1.0",
	"Accept":            "application/json",
	"X-Requested-With":  "XMLHttpRequest",
}
