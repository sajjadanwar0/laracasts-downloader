package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	_ "github.com/sajjadanwar0/laracasts-dl/internal/utils"
	"github.com/sajjadanwar0/laracasts-dl/internal/vimeo"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	_ "strings"

	"github.com/sajjadanwar0/laracasts-dl/internal/config"
)

type Downloader struct {
	Client   *http.Client
	Vimeo    *vimeo.Client
	BasePath string
}

func New() (*Downloader, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Jar: jar}

	return &Downloader{
		Client:   client,
		Vimeo:    vimeo.NewClient(client),
		BasePath: "downloads",
	}, nil
}

func (d *Downloader) Login(email, password string) error {
	printBox("Authenticating")

	// Get initial page and XSRF token
	xsrfToken, err := d.getXSRFToken()
	if err != nil {
		return fmt.Errorf("failed to get XSRF token: %v", err)
	}

	// Prepare login data
	auth := map[string]interface{}{
		"email":    email,
		"password": password,
		"remember": true,
	}

	jsonData, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("failed to marshal auth data: %v", err)
	}

	// Send login request
	loginURL := config.LaracastsBaseUrl + config.LaracastsPostLoginPath
	req, err := http.NewRequest("POST", loginURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create login request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-XSRF-TOKEN", xsrfToken)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", config.LaracastsBaseUrl)

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed login request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox("Failed to close body")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("> Logged in as %s\n", email)
	return nil
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

type Episode struct {
	Title   string
	VimeoId string
	Number  int
}

func SanitizeFilename(filename string) string {
	invalids := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename
	for _, char := range invalids {
		result = strings.ReplaceAll(result, char, "-")
	}
	return result
}

func (d *Downloader) downloadEpisode(slug string, episode Episode) error {
	return d.DownloadEpisode("series", slug, episode)
}

func (d *Downloader) DownloadEpisode(contentType, slug string, episode Episode) error {
	filename := fmt.Sprintf("%02d-%s.mp4", episode.Number, SanitizeFilename(episode.Title))
	fullPath := filepath.Join(d.BasePath, contentType, slug, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Get video configuration
	videoConfig, err := d.Vimeo.GetVideoConfig(episode.VimeoId)
	if err != nil {
		return fmt.Errorf("failed to get video config: %v", err)
	}

	// Download the video
	return d.Vimeo.DownloadVideo(videoConfig, fullPath)
}

func (d *Downloader) ensureOutputDir(contentType, slug string) (string, error) {
	outputDir := filepath.Join(d.BasePath, contentType, slug)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}
	return outputDir, nil
}

func printBox(text string) {
	fmt.Println("====================================")
	fmt.Println(text)
	fmt.Println("====================================")
}
