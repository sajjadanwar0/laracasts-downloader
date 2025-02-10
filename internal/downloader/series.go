package downloader

import (
	"encoding/json"
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"github.com/sajjadanwar0/laracasts-dl/internal/utils"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

func (d *Downloader) DownloadSeries(seriesSlug string) error {
	printBox(fmt.Sprintf("Downloading series: %s", seriesSlug))

	seriesURL := fmt.Sprintf("%s/%s", config.LaracastsBaseUrl, seriesSlug)

	// Create request with Inertia headers
	req, err := http.NewRequest("GET", seriesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.InertiaHeaders {
		req.Header.Set(k, v)
	}

	token, _ := d.getXSRFToken()
	if token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed initial request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox(fmt.Sprintf("Failed to close response body: %v", err))
		}
	}(resp.Body)

	// Handle 409 status
	if resp.StatusCode == http.StatusConflict {
		req, err = http.NewRequest("GET", seriesURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create regular request: %v", err)
		}

		for k, v := range config.DefaultHeaders {
			req.Header.Set(k, v)
		}

		resp, err = d.Client.Do(req)
		if err != nil {
			return fmt.Errorf("failed regular request: %v", err)
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				printBox(fmt.Sprintf("Failed to close response body: %v", err))
			}
		}(resp.Body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	jsonContent, err := extractSeriesJSON(string(body))
	if err != nil {
		return fmt.Errorf("failed to extract series data: %v", err)
	}

	var data struct {
		Props struct {
			Series struct {
				Title    string `json:"title"`
				Chapters []struct {
					Episodes []struct {
						Title    string `json:"title"`
						VimeoId  string `json:"vimeoId"`
						Position int    `json:"position"`
					} `json:"episodes"`
				} `json:"chapters"`
			} `json:"series"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil {
		return fmt.Errorf("failed to parse series data: %v", err)
	}

	// Create series directory
	outputDir := filepath.Join(d.BasePath, "series", seriesSlug)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Download episodes
	var totalEpisodes int
	for i, chapter := range data.Props.Series.Chapters {
		fmt.Printf("\nProcessing Chapter %d\n", i+1)
		for _, episode := range chapter.Episodes {
			if episode.VimeoId == "" {
				continue
			}
			totalEpisodes++

			fmt.Printf("\nDownloading episode %d: %s\n", episode.Position, episode.Title)
			if err := d.downloadEpisode(seriesSlug, Episode{
				Title:   episode.Title,
				VimeoId: episode.VimeoId,
				Number:  episode.Position,
			}); err != nil {
				fmt.Printf("Failed to download episode: %v\n", err)
				continue
			}
		}
	}

	fmt.Printf("\nFinished downloading %d episodes\n", totalEpisodes)
	return nil
}

func (d *Downloader) DownloadAllSeries() error {
	printBox("Downloading all series")

	page := 1
	hasMore := true

	for hasMore {
		fmt.Printf("\nFetching page %d...\n", page)

		series, nextPage, err := d.getSeriesPage(page)
		if err != nil {
			return fmt.Errorf("failed to fetch series page %d: %v", page, err)
		}

		for _, s := range series {
			if err := d.DownloadSeries(s.Slug); err != nil {
				fmt.Printf("Error downloading series '%s': %v\n", s.Title, err)
			}
		}

		hasMore = nextPage != ""
		page++
	}

	return nil
}

func (d *Downloader) getSeriesPage(page int) ([]struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}, string, error) {
	url := fmt.Sprintf("%s%s?page=%d", config.LaracastsBaseUrl, config.LaracastsSeriesPath, page)

	// First try with Inertia headers
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.InertiaHeaders {
		req.Header.Set(k, v)
	}

	token, _ := d.getXSRFToken()
	if token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to execute request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox(fmt.Sprintf("Failed to close response body: %v", err))
		}
	}(resp.Body)

	// Handle 409 by retrying with regular headers
	if resp.StatusCode == http.StatusConflict {
		req, err = http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create regular request: %v", err)
		}

		for k, v := range config.DefaultHeaders {
			req.Header.Set(k, v)
		}

		resp, err = d.Client.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("failed regular request: %v", err)
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				printBox(fmt.Sprintf("Failed to close response body: %v", err))
			}
		}(resp.Body)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %v", err)
	}

	jsonContent, err := extractSeriesJSON(string(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract JSON: %v", err)
	}

	var response struct {
		Props struct {
			Series []struct {
				Title string `json:"title"`
				Slug  string `json:"slug"`
			} `json:"series"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &response); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %v", err)
	}

	return response.Props.Series, response.Props.Links.Next, nil
}

func extractSeriesJSON(content string) (string, error) {
	// Try data-page attribute first
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	if matches := dataPageRe.FindStringSubmatch(content); len(matches) > 1 {
		decoded := html.UnescapeString(matches[1])
		return utils.CleanJSON(decoded)
	}

	// Try script tag
	scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
	if matches := scriptRe.FindStringSubmatch(content); len(matches) > 1 {
		return utils.CleanJSON(html.UnescapeString(matches[1]))
	}

	return "", fmt.Errorf("no series data found in response")
}
