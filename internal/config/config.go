package config

// Laracasts base URLs and paths
const (
	LaracastsBaseUrl       = "https://laracasts.com"
	LaracastsPostLoginPath = "/sessions"
	LaracastsSeriesPath    = "/series"
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

// DownloadConfig Download configuration
type DownloadConfig struct {
	BasePath          string
	ConcurrentWorkers int
	RetryAttempts     int
	RetryDelay        int   // seconds
	MinFileSize       int64 // bytes
	PageDelay         int   // seconds
	DownloadDelay     int   // seconds
}
