package config

// Laracasts base URLs and paths
const (
	LaracastsBaseUrl       = "https://laracasts.com"
	LaracastsPostLoginPath = "/sessions"
	LaracastsSeriesPath    = "/series"
	LaracastsBitsPath      = "/bits"
	LaracastsTopicsPath    = "/topics"
	LaracastsBrowsePath    = "/browse"
)

// ContentType represents a type of content in Laracasts
type ContentType struct {
	Path       string // API path for the content
	Name       string // Display name
	PathInURL  string // Path segment in URLs
	FolderName string // Folder name for downloads
	HasTeacher bool   // Whether content can be filtered by teacher
}

// ContentTypes maps flag keys to content type configurations
var ContentTypes = map[string]ContentType{
	"s": {
		Path:       LaracastsSeriesPath,
		Name:       "Series",
		PathInURL:  "series",
		FolderName: "series",
		HasTeacher: false,
	},
	"l": {
		Path:       LaracastsBitsPath,
		Name:       "Larabits",
		PathInURL:  "bits",
		FolderName: "larabits",
		HasTeacher: true,
	},
	"t": {
		Path:       LaracastsTopicsPath,
		Name:       "Topics",
		PathInURL:  "topics",
		FolderName: "topics",
		HasTeacher: false,
	},
}

// HTTP request headers
var DefaultHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

var JsonHeaders = map[string]string{
	"Content-Type":     "application/json",
	"Accept":           "application/json",
	"X-Requested-With": "XMLHttpRequest",
}

var InertiaHeaders = map[string]string{
	"X-Inertia":         "true",
	"X-Inertia-Version": "1.0",
	"Accept":            "application/json",
	"X-Requested-With":  "XMLHttpRequest",
}

// Download configuration
type DownloadConfig struct {
	BasePath          string
	ConcurrentWorkers int
	RetryAttempts     int
	RetryDelay        int   // seconds
	MinFileSize       int64 // bytes
	PageDelay         int   // seconds
	DownloadDelay     int   // seconds
}

var DefaultDownloadConfig = DownloadConfig{
	BasePath:          "downloads",
	ConcurrentWorkers: 4,
	RetryAttempts:     3,
	RetryDelay:        5,
	MinFileSize:       1024 * 1024, // 1MB
	PageDelay:         1,
	DownloadDelay:     2,
}

// FFmpegConfig FFmpeg configuration
var FFmpegConfig = struct {
	DefaultArgs []string
}{
	DefaultArgs: []string{
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-y",
	},
}

// Progress bar configuration
type ProgressConfig struct {
	Width     int
	Throttle  int // milliseconds
	ShowBytes bool
	ShowCount bool
}

var DefaultProgressConfig = ProgressConfig{
	Width:     30,
	Throttle:  65,
	ShowBytes: true,
	ShowCount: true,
}
