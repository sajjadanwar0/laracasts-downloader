package downloader

import (
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/cache"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"github.com/sajjadanwar0/laracasts-dl/internal/vimeo"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxEpisodeWorkers = 15  // Concurrent episode downloads
	JobBufferSize     = 200 // Buffer for job channel
	ResultsBufferSize = 200 // Buffer for results channel

)

type Downloader struct {
	Client   *http.Client
	Vimeo    *vimeo.Client
	BasePath string
	Cache    *cache.Cache
}

type Episode struct {
	Title   string
	VimeoId string
	Number  int
}

//downloader.go

func New() (*Downloader, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	// Use the path directly from config/env
	basePath := config.GetDownloadPath()

	// Create downloads directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create downloads directory: %v", err)
	}

	// Initialize cache
	newCache, err := cache.NewCache(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cache: %v", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 100,
		},
	}

	return &Downloader{
		Client:   client,
		Vimeo:    vimeo.NewClient(client),
		BasePath: basePath,
		Cache:    newCache,
	}, nil
}

func (d *Downloader) getXSRFToken() (string, error) {
	req, err := http.NewRequest("GET", config.LaracastsBaseUrl, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox("Failed to close body")
		}
	}(resp.Body)

	laracastsURL, _ := url.Parse(config.LaracastsBaseUrl)
	cookies := d.Client.Jar.Cookies(laracastsURL)

	for _, cookie := range cookies {
		if cookie.Name == "XSRF-TOKEN" {
			decoded, err := url.QueryUnescape(cookie.Value)
			if err == nil {
				return decoded, nil
			}
		}
	}

	return "", fmt.Errorf("XSRF token not found in cookies")
}

func (d *Downloader) downloadEpisode(outputDir string, episode Episode) error {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		err := d.tryDownload(outputDir, episode)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(i*i) * time.Second)
	}
	return fmt.Errorf("failed after %d retries", maxRetries)
}

func (d *Downloader) tryDownload(outputDir string, episode Episode) error {
	filename := fmt.Sprintf("%02d-%s.mp4", episode.Number, sanitizeFilename(episode.Title))
	outputPath := filepath.Join(outputDir, filename) // Use the provided outputDir

	// Check if file already exists and is complete
	if info, err := os.Stat(outputPath); err == nil && info.Size() > 0 {
		// File exists and has content
		return nil
	}

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Get video configuration
	videoConfig, err := d.Vimeo.GetVideoConfig(episode.VimeoId)
	if err != nil {
		return fmt.Errorf("failed to get video config: %v", err)
	}

	// Download the video
	return d.Vimeo.DownloadVideo(videoConfig, outputPath)
}

func printBox(text string) {
	width := len(text) + 4
	line := strings.Repeat("=", width)
	fmt.Printf("\n%s\n  %s\n%s\n", line, text, line)
}
