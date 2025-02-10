package downloader

import (
	"encoding/json"
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"html"
	"io"
	"net/http"
	_ "net/url"

	"os"
	"path/filepath"
	"regexp"
	"strings"
	_ "strings"
	"sync"
	"time"
)

type SeriesMetadata struct {
	Title     string    `json:"title"`
	Chapters  []Chapter `json:"chapters"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Chapter struct {
	Title    string    `json:"title"`
	Episodes []Episode `json:"episodes"`
}

type DownloadState struct {
	Completed map[string]bool `json:"completed"`
	LastSync  time.Time       `json:"last_sync"`
}

func (d *Downloader) DownloadSeries(seriesSlug string) error {
	printBox(fmt.Sprintf("Downloading series: %s", seriesSlug))

	// Try to get series metadata from cache
	var seriesData SeriesMetadata
	cacheKey := fmt.Sprintf("series_%s", seriesSlug)

	found, err := d.Cache.Get(cacheKey, &seriesData)
	if err != nil {
		fmt.Printf("Cache error: %v, fetching fresh data\n", err)
		found = false
	}

	// Fetch fresh data if not found in cache or stale
	if !found || d.Cache.IsStale(cacheKey, 24*time.Hour) {
		fmt.Println("Fetching series metadata from Laracasts...")

		seriesURL := fmt.Sprintf("%s/%s", config.LaracastsBaseUrl, seriesSlug)
		jsonData, err := d.fetchSeriesData(seriesURL)
		if err != nil {
			return fmt.Errorf("failed to fetch series data: %v", err)
		}

		var rawData struct {
			Props struct {
				Series struct {
					Title    string `json:"title"`
					Chapters []struct {
						Title    string `json:"title"`
						Episodes []struct {
							Title    string `json:"title"`
							VimeoId  string `json:"vimeoId"`
							Position int    `json:"position"`
						} `json:"episodes"`
					} `json:"chapters"`
				} `json:"series"`
			} `json:"props"`
		}

		if err := json.Unmarshal([]byte(jsonData), &rawData); err != nil {
			return fmt.Errorf("failed to parse series data: %v", err)
		}

		// Convert to metadata structure
		seriesData = SeriesMetadata{
			Title:     rawData.Props.Series.Title,
			UpdatedAt: time.Now(),
		}

		for _, chapter := range rawData.Props.Series.Chapters {
			var episodes []Episode
			for _, ep := range chapter.Episodes {
				if ep.VimeoId != "" {
					episodes = append(episodes, Episode{
						Title:   ep.Title,
						VimeoId: ep.VimeoId,
						Number:  ep.Position,
					})
				}
			}

			seriesData.Chapters = append(seriesData.Chapters, Chapter{
				Title:    chapter.Title,
				Episodes: episodes,
			})
		}

		// Cache the series metadata
		if err := d.Cache.Set(cacheKey, seriesData); err != nil {
			fmt.Printf("Warning: Failed to cache series metadata: %v\n", err)
		}
	} else {
		fmt.Println("Using cached series metadata")
	}

	// Load or initialize download state
	state, err := d.loadDownloadState(seriesSlug)
	if err != nil {
		state = &DownloadState{
			Completed: make(map[string]bool),
			LastSync:  time.Now(),
		}
	}

	// Create series directory
	outputDir := filepath.Join(d.BasePath, "series", seriesSlug)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Prepare episodes for download
	var episodesToDownload []Episode
	var totalEpisodes int

	fmt.Printf("\nSeries: %s\n", seriesData.Title)

	for chapterIdx, chapter := range seriesData.Chapters {
		fmt.Printf("\nChapter %d: %s\n", chapterIdx+1, chapter.Title)
		for _, episode := range chapter.Episodes {
			totalEpisodes++

			if state.Completed[episode.VimeoId] {
				fmt.Printf("- [✓] Episode %d: %s (already downloaded)\n",
					episode.Number, episode.Title)
				continue
			}

			episodesToDownload = append(episodesToDownload, episode)
			fmt.Printf("- [ ] Episode %d: %s (queued)\n",
				episode.Number, episode.Title)
		}
	}

	if len(episodesToDownload) == 0 {
		fmt.Printf("\nAll %d episodes already downloaded!\n", totalEpisodes)
		return nil
	}

	fmt.Printf("\nPreparing to download %d/%d episodes with %d workers\n",
		len(episodesToDownload), totalEpisodes, MaxEpisodeWorkers)

	// Create worker pool
	jobs := make(chan Episode, JobBufferSize)
	results := make(chan struct {
		episode Episode
		err     error
	}, ResultsBufferSize)

	// Start workers
	var wg sync.WaitGroup
	for w := 1; w <= MaxEpisodeWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for episode := range jobs {
				fmt.Printf("\nWorker %d starting download: Episode %d - %s\n",
					id, episode.Number, episode.Title)

				err := d.downloadEpisode(seriesSlug, episode)
				results <- struct {
					episode Episode
					err     error
				}{episode, err}

				if err != nil {
					fmt.Printf("❌ Worker %d failed episode %d: %v\n",
						id, episode.Number, err)
				} else {
					fmt.Printf("✅ Worker %d completed episode %d: %s\n",
						id, episode.Number, episode.Title)
				}
			}
		}(w)
	}

	// Send jobs to workers
	go func() {
		for _, episode := range episodesToDownload {
			jobs <- episode
		}
		close(jobs)
	}()

	// Wait for all workers
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	var successCount, failedCount int
	for result := range results {
		if result.err == nil {
			successCount++
			state.Completed[result.episode.VimeoId] = true
			if err := d.saveDownloadState(seriesSlug, state); err != nil {
				fmt.Printf("Warning: Failed to save download state: %v\n", err)
			}
		} else {
			failedCount++
		}

		completed := successCount + failedCount
		fmt.Printf("\rProgress: %.1f%% (%d/%d) ✅ Success: %d ❌ Failed: %d",
			float64(completed)/float64(len(episodesToDownload))*100,
			completed, len(episodesToDownload),
			successCount, failedCount)
	}

	fmt.Printf("\n\nDownload Summary for %s:\n", seriesData.Title)
	fmt.Printf("Total Episodes: %d\n", totalEpisodes)
	fmt.Printf("Previously Downloaded: %d\n", totalEpisodes-len(episodesToDownload))
	fmt.Printf("Successfully Downloaded: %d\n", successCount)
	fmt.Printf("Failed Downloads: %d\n", failedCount)

	if failedCount > 0 {
		return fmt.Errorf("some episodes failed to download")
	}

	return nil
}

func (d *Downloader) fetchSeriesData(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
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
		return "", fmt.Errorf("failed request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox("Failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		req, err = http.NewRequest("GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create regular request: %v", err)
		}

		for k, v := range config.DefaultHeaders {
			req.Header.Set(k, v)
		}

		resp, err = d.Client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed regular request: %v", err)
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				printBox("Failed to close response body")
			}
		}(resp.Body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	return extractSeriesJSON(string(body))
}

func extractSeriesJSON(content string) (string, error) {
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	if matches := dataPageRe.FindStringSubmatch(content); len(matches) > 1 {
		decoded := html.UnescapeString(matches[1])
		return decoded, nil
	}

	scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
	if matches := scriptRe.FindStringSubmatch(content); len(matches) > 1 {
		return html.UnescapeString(matches[1]), nil
	}

	return "", fmt.Errorf("no series data found in response")
}

func (d *Downloader) loadDownloadState(seriesSlug string) (*DownloadState, error) {
	var state DownloadState
	found, err := d.Cache.Get(fmt.Sprintf("download_state_%s", seriesSlug), &state)
	if err != nil || !found {
		return nil, fmt.Errorf("no download state found")
	}
	return &state, nil
}

func (d *Downloader) saveDownloadState(seriesSlug string, state *DownloadState) error {
	state.LastSync = time.Now()
	return d.Cache.Set(fmt.Sprintf("download_state_%s", seriesSlug), state)
}

func (d *Downloader) getSeriesPage(page int) ([]struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}, string, error) {
	browseURL := fmt.Sprintf("%s/browse/series?page=%d", config.LaracastsBaseUrl, page)
	fmt.Printf("Fetching URL: %s\n", browseURL)

	// First try with Inertia headers
	req, err := http.NewRequest("GET", browseURL, nil)
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
		return nil, "", fmt.Errorf("failed request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %v", err)
	}

	// Extract data-page content from HTML
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	matches := dataPageRe.FindSubmatch(body)
	if len(matches) < 2 {
		// Try alternate script tag pattern
		scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
		matches = scriptRe.FindSubmatch(body)
		if len(matches) < 2 {
			return nil, "", fmt.Errorf("could not find series data in response")
		}
	}

	// Decode HTML entities and unescape JSON
	decodedContent := html.UnescapeString(string(matches[1]))

	// Parse the Inertia JSON response
	var response struct {
		Props struct {
			Series []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
				Path  string `json:"path"`
				Slug  string `json:"slug"`
			} `json:"items"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(decodedContent), &response); err != nil {
		// Debug output
		fmt.Printf("Failed to parse JSON content: %s\n", decodedContent)
		return nil, "", fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Convert to expected format
	var series []struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}

	for _, s := range response.Props.Series {
		slug := s.Slug
		if slug == "" {
			// Extract slug from path
			slug = strings.TrimPrefix(s.Path, "/series/")
		}

		series = append(series, struct {
			Title string `json:"title"`
			Slug  string `json:"slug"`
		}{
			Title: s.Title,
			Slug:  slug,
		})
	}

	var nextPage string
	if response.Props.Links.Next != "" {
		nextPage = response.Props.Links.Next
	}

	fmt.Printf("Found %d series on this page\n", len(series))
	for _, s := range series {
		fmt.Printf("- %s (%s)\n", s.Title, s.Slug)
	}

	return series, nextPage, nil
}

func (d *Downloader) DownloadAllSeries() error {
	printBox("Downloading all series")

	page := 1
	hasMore := true
	var totalSeries, completedSeries, failedSeries int
	downloadedSeries := make(map[string]bool) // Track downloaded series to avoid duplicates

	// Create a channel to limit concurrent downloads
	sem := make(chan bool, 3) // Limit to 3 concurrent downloads
	var wg sync.WaitGroup

	for hasMore {
		fmt.Printf("\n📚 Fetching page %d of series catalog...\n", page)

		series, nextPage, err := d.getSeriesPage(page)
		if err != nil {
			return fmt.Errorf("failed to fetch series page %d: %v", page, err)
		}

		totalOnPage := len(series)
		if totalOnPage == 0 {
			break
		}

		totalSeries += totalOnPage
		fmt.Printf("\nFound %d series on page %d\n", totalOnPage, page)

		// Process series concurrently
		for idx, s := range series {
			// Skip if already downloaded
			if downloadedSeries[s.Slug] {
				continue
			}
			downloadedSeries[s.Slug] = true

			wg.Add(1)
			sem <- true // Acquire semaphore

			go func(idx int, s struct {
				Title string `json:"title"`
				Slug  string `json:"slug"`
			}) {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore

				fmt.Printf("\n[%d/%d] 📺 Downloading series: %s\n", idx+1, totalOnPage, s.Title)

				if err := d.DownloadSeries(s.Slug); err != nil {
					fmt.Printf("❌ Error downloading series '%s': %v\n", s.Title, err)
					failedSeries++
				} else {
					completedSeries++
					fmt.Printf("✅ Completed series: %s\n", s.Title)
				}
			}(idx, s)
		}

		// Check for next page
		if nextPage == "" {
			hasMore = false
		} else {
			page++
		}
	}

	// Wait for all downloads to complete
	wg.Wait()

	fmt.Printf("\n🎉 Download Summary:\n")
	fmt.Printf("Total Series Found: %d\n", totalSeries)
	fmt.Printf("Series Completed: %d\n", completedSeries)
	fmt.Printf("Series Failed: %d\n", failedSeries)

	if failedSeries > 0 {
		return fmt.Errorf("some series failed to download (%d failed)", failedSeries)
	}

	return nil
}
