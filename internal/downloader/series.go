package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DownloadState struct {
	Completed map[string]bool `json:"completed"`
	LastSync  time.Time       `json:"last_sync"`
}

// Add this new struct to the top of series.go
type TopicSeries struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Path      string `json:"path"`
	TopicPath string `json:"topic_path"`
	TopicName string `json:"topic_name"`
}

func (d *Downloader) getTopicSeries(topicURL string, topicName string) ([]TopicSeries, error) {
	fmt.Printf("Fetching series from: %s\n", topicURL)

	req, err := http.NewRequest("GET", topicURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("topic page not found (404)")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Parse page data
	var pageData struct {
		Props struct {
			Topic struct {
				Name   string `json:"name"`
				Path   string `json:"path"`
				Series []struct {
					ID           int    `json:"id"`
					Title        string `json:"title"`
					Path         string `json:"path"`
					Slug         string `json:"slug"`
					EpisodeCount int    `json:"episodeCount"`
					Topics       []struct {
						Name string `json:"name"`
						Path string `json:"path"`
					} `json:"topics"`
				} `json:"series"`
			} `json:"topic"`
		} `json:"props"`
	}

	jsonData := extractPageJSON(body)
	if jsonData == "" {
		return nil, fmt.Errorf("no page data found")
	}

	if err := json.Unmarshal([]byte(jsonData), &pageData); err != nil {
		return nil, fmt.Errorf("failed to parse page data: %v", err)
	}

	var series []TopicSeries
	downloadedSlugs := make(map[string]bool)

	// Process series that are directly listed under this topic
	for _, s := range pageData.Props.Topic.Series {
		if s.Title == "" || downloadedSlugs[s.Slug] {
			continue
		}

		// Get the clean slug
		slug := s.Slug
		if s.Path != "" {
			slug = strings.TrimPrefix(s.Path, "/series/")
		}

		// Ensure proper series slug format
		if !strings.HasPrefix(slug, "series/") {
			slug = fmt.Sprintf("series/%s", slug)
		}

		series = append(series, TopicSeries{
			Title:     s.Title,
			Slug:      slug,
			Path:      s.Path,
			TopicPath: pageData.Props.Topic.Path,
			TopicName: topicName,
		})

		downloadedSlugs[s.Slug] = true
		fmt.Printf("Found series for topic %s: %s (slug: %s)\n",
			topicName, s.Title, slug)
	}

	if len(series) == 0 {
		return nil, fmt.Errorf("no series found for topic '%s'", topicName)
	}

	fmt.Printf("Successfully found %d series for topic '%s'\n",
		len(series), topicName)
	return series, nil
}

type SeriesMetadata struct {
	Title     string    `json:"title"`
	Chapters  []Chapter `json:"chapters"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Chapter struct {
	Title    string    `json:"title"`
	Episodes []Episode `json:"episodes"`
}

// Helper function to get consistent folder names
func getSeriesFolderName(series TopicSeries) string {
	// Use the series title for folder name, properly sanitized
	folderName := sanitizeFilename(series.Title)

	// Convert to lowercase
	folderName = strings.ToLower(folderName)

	// Replace multiple dashes with single dash
	folderName = regexp.MustCompile(`-+`).ReplaceAllString(folderName, "-")

	// Trim dashes from start and end
	folderName = strings.Trim(folderName, "-")

	return folderName
}

// Main function to handle series download
func (d *Downloader) handleSeriesDownload(topicsDir string, series TopicSeries, downloadedSeries map[string]string) error {
	// Get consistent folder name for the topic and series
	topicFolderName := sanitizeFilename(series.TopicName)
	seriesFolderName := getSeriesFolderName(series)

	// Create full path using consistent naming
	// This now creates: topics/topic-name/series-name
	seriesDir := filepath.Join(topicsDir, topicFolderName, seriesFolderName)

	// Check if this series has already been downloaded to another topic
	if existingPath, exists := downloadedSeries[series.Slug]; exists {
		fmt.Printf("Series '%s' already exists at '%s', creating symlink...\n",
			series.Title, existingPath)

		// Create parent directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(seriesDir), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}

		// Create relative symlink
		relPath, err := filepath.Rel(filepath.Dir(seriesDir), existingPath)
		if err != nil {
			return fmt.Errorf("failed to create relative path: %v", err)
		}

		// Remove existing symlink or folder if it exists
		if _, err := os.Lstat(seriesDir); err == nil {
			os.RemoveAll(seriesDir)
		}

		if err := os.Symlink(relPath, seriesDir); err != nil {
			return fmt.Errorf("failed to create symlink: %v", err)
		}

		return nil
	}

	// This is the first time we're downloading this series
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create series directory: %v", err)
	}

	// Set BasePath to the series directory
	d.BasePath = seriesDir

	// Download series content directly to the folder
	if err := d.downloadSeriesContent(series.Slug); err != nil {
		return fmt.Errorf("failed to download series: %v", err)
	}

	// Record this series as downloaded
	downloadedSeries[series.Slug] = seriesDir

	return nil
}

// Function to download series content without creating extra directories
func (d *Downloader) downloadSeriesContent(seriesSlug string) error {
	// Get series metadata
	var seriesData SeriesMetadata
	cacheKey := fmt.Sprintf("series_%s", strings.TrimPrefix(seriesSlug, "series/"))

	found, err := d.Cache.Get(cacheKey, &seriesData)
	if err != nil {
		fmt.Printf("Cache error: %v, fetching fresh data\n", err)
		found = false
	}

	if !found || d.Cache.IsStale(cacheKey, 3600*24*7) {
		// Fetch and parse series data
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
	}

	// Create worker pool for episode downloads
	jobs := make(chan struct {
		episode   Episode
		outputDir string
	}, JobBufferSize)
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
			for job := range jobs {
				fmt.Printf("\nWorker %d starting download: Episode %d - %s\n",
					id, job.episode.Number, job.episode.Title)

				err := d.downloadEpisode(job.outputDir, job.episode)
				time.Sleep(time.Millisecond)

				if err != nil {
					fmt.Printf("❌ Worker %d failed episode %d: %v\n",
						id, job.episode.Number, err)
				} else {
					fmt.Printf("✅ Worker %d completed episode %d: %s\n",
						id, job.episode.Number, job.episode.Title)
				}

				results <- struct {
					episode Episode
					err     error
				}{job.episode, err}
			}
		}(w)
	}

	// Send jobs to workers
	totalEpisodes := 0
	for _, chapter := range seriesData.Chapters {
		fmt.Printf("\nChapter: %s\n", chapter.Title)
		for _, episode := range chapter.Episodes {
			totalEpisodes++
			jobs <- struct {
				episode   Episode
				outputDir string
			}{episode, d.BasePath}
		}
	}
	close(jobs)

	// Wait for all workers
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	var failures int32
	var successCount int32
	for result := range results {
		if result.err != nil {
			atomic.AddInt32(&failures, 1)
		} else {
			atomic.AddInt32(&successCount, 1)
		}

		completed := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&failures)
		fmt.Printf("\rProgress: %.1f%% (%d/%d) ✅ Success: %d ❌ Failed: %d",
			float64(completed)/float64(totalEpisodes)*100,
			completed, totalEpisodes,
			atomic.LoadInt32(&successCount),
			atomic.LoadInt32(&failures))
	}

	fmt.Println() // New line after progress bar

	if failures > 0 {
		return fmt.Errorf("%d episodes failed to download", failures)
	}

	return nil
}

// Update the sanitizeFilename function to be more consistent
func sanitizeFilename(filename string) string {
	// Convert to lowercase
	filename = strings.ToLower(filename)

	// Replace spaces and invalid characters with dashes
	invalids := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", " "}
	result := filename

	for _, char := range invalids {
		result = strings.ReplaceAll(result, char, "-")
	}

	// Replace multiple dashes with single dash
	result = regexp.MustCompile(`-+`).ReplaceAllString(result, "-")

	// Trim dashes from start and end
	result = strings.Trim(result, "-")

	return result
}

func extractPageJSON(body []byte) string {
	// First try finding script tag with page data
	scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
	matches := scriptRe.FindSubmatch(body)

	if len(matches) > 1 {
		return html.UnescapeString(string(matches[1]))
	}

	// Try data-page attribute as fallback
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	matches = dataPageRe.FindSubmatch(body)
	if len(matches) > 1 {
		return html.UnescapeString(string(matches[1]))
	}

	return ""
}

func (d *Downloader) DownloadAllByTopics() error {
	printBox("Downloading all series organized by topics")

	// Store original base path
	originalBasePath := d.BasePath

	// Create tracking map for downloaded series
	downloadedSeries := make(map[string]string) // maps series slug to its directory
	var downloadMutex sync.Mutex

	// Get the browse page with retries
	var body []byte
	var err error
	maxRetries := 3

	browseURL := fmt.Sprintf("%s/browse/all", config.LaracastsBaseUrl)
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", browseURL, nil)
		if err != nil {
			continue
		}

		for k, v := range config.DefaultHeaders {
			req.Header.Set(k, v)
		}

		resp, err := d.Client.Do(req)
		if err != nil {
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err == nil {
			break
		}
		time.Sleep(time.Second * time.Duration(i+1))
	}

	if err != nil {
		return fmt.Errorf("failed to fetch browse page after %d attempts: %v", maxRetries, err)
	}

	// Parse the page data
	jsonData := extractPageJSON(body)
	if jsonData == "" {
		return fmt.Errorf("no page data found")
	}

	var pageDataStruct struct {
		Props struct {
			Topics []struct {
				Name         string `json:"name"`
				EpisodeCount int    `json:"episode_count"`
				SeriesCount  int    `json:"series_count"`
				Path         string `json:"path"`
			} `json:"topics"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonData), &pageDataStruct); err != nil {
		return fmt.Errorf("failed to parse JSON data: %v", err)
	}

	// Create topics directory
	topicsDir := filepath.Join(originalBasePath, "topics")
	if err := os.MkdirAll(topicsDir, 0755); err != nil {
		return fmt.Errorf("failed to create topics directory: %v", err)
	}

	// Process each topic
	var wg sync.WaitGroup
	sem := make(chan bool, 4) // Limit concurrent topics
	var mu sync.Mutex
	var (
		completedTopics int32
		failedTopics    int32
	)

	for i, topic := range pageDataStruct.Props.Topics {
		wg.Add(1)
		sem <- true // Acquire semaphore

		go func(idx int, topic struct {
			Name         string
			EpisodeCount int
			SeriesCount  int
			Path         string
		}) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			// Add delay between topics
			time.Sleep(time.Second * 2)

			mu.Lock()
			fmt.Printf("\n[%d/%d] 📚 Processing topic: %s\n",
				idx+1, len(pageDataStruct.Props.Topics), topic.Name)
			mu.Unlock()

			// Get series for this topic
			series, err := d.getTopicSeries(topic.Path, topic.Name)
			if err != nil {
				mu.Lock()
				fmt.Printf("❌ Error getting series for topic '%s': %v\n", topic.Name, err)
				mu.Unlock()
				atomic.AddInt32(&failedTopics, 1)
				return
			}

			// Download each series
			var topicFailures int32
			for _, s := range series {
				downloadMutex.Lock()
				err := d.handleSeriesDownload(topicsDir, s, downloadedSeries)
				downloadMutex.Unlock()

				if err != nil {
					mu.Lock()
					fmt.Printf("❌ Error processing series '%s': %v\n", s.Title, err)
					mu.Unlock()
					atomic.AddInt32(&topicFailures, 1)
				}
			}

			if topicFailures > 0 {
				atomic.AddInt32(&failedTopics, 1)
			} else {
				atomic.AddInt32(&completedTopics, 1)
			}

			mu.Lock()
			fmt.Printf("✅ Completed topic: %s\n", topic.Name)
			fmt.Printf("\nProgress: %.1f%% (%d/%d) Topics Completed\n",
				float64(atomic.LoadInt32(&completedTopics))/float64(len(pageDataStruct.Props.Topics))*100,
				atomic.LoadInt32(&completedTopics),
				len(pageDataStruct.Props.Topics))
			mu.Unlock()

		}(i, struct {
			Name         string
			EpisodeCount int
			SeriesCount  int
			Path         string
		}(topic))
	}

	wg.Wait()

	// Save download mapping for debugging
	downloadMap := filepath.Join(topicsDir, "series_locations.json")
	if mapData, err := json.MarshalIndent(downloadedSeries, "", "  "); err == nil {
		_ = os.WriteFile(downloadMap, mapData, 0644)
	}

	// Restore original BasePath
	d.BasePath = originalBasePath

	// Print summary
	completed := atomic.LoadInt32(&completedTopics)
	failed := atomic.LoadInt32(&failedTopics)

	fmt.Printf("\n🎉 Download Summary:\n")
	fmt.Printf("Total Topics Found: %d\n", len(pageDataStruct.Props.Topics))
	fmt.Printf("Topics Completed: %d\n", completed)
	fmt.Printf("Topics Failed: %d\n", failed)

	if failed > 0 {
		return fmt.Errorf("%d topics failed to process", failed)
	}

	return nil
}
func (d *Downloader) extractSeriesFromJSON(body []byte, topicName string) ([]struct {
	Title string
	Slug  string
}, error) {
	// Try to find the page data in JSON
	var jsonData string

	// First try the script tag
	scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
	if matches := scriptRe.FindSubmatch(body); len(matches) > 1 {
		jsonData = html.UnescapeString(string(matches[1]))
	} else {
		// Try data-page attribute as fallback
		dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
		if matches := dataPageRe.FindSubmatch(body); len(matches) > 1 {
			jsonData = html.UnescapeString(string(matches[1]))
		}
	}

	if jsonData == "" {
		return nil, fmt.Errorf("no series data found in page")
	}

	var pageDataStruct struct {
		Props struct {
			Topic struct {
				Series []struct {
					Title string `json:"title"`
					Path  string `json:"path"`
					Slug  string `json:"slug"`
				} `json:"series"`
			} `json:"topic"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonData), &pageDataStruct); err != nil {
		return nil, fmt.Errorf("failed to parse JSON data: %v", err)
	}

	var series []struct {
		Title string
		Slug  string
	}

	for _, s := range pageDataStruct.Props.Topic.Series {
		if s.Title == "" {
			continue
		}

		slug := s.Slug
		if s.Path != "" {
			slug = strings.TrimPrefix(s.Path, "/series/")
		}

		// Ensure slug has "series/" prefix
		if !strings.HasPrefix(slug, "series/") {
			slug = fmt.Sprintf("series/%s", slug)
		}

		series = append(series, struct {
			Title string
			Slug  string
		}{
			Title: strings.TrimSpace(s.Title),
			Slug:  strings.TrimSpace(slug),
		})

		fmt.Printf("Found series (from JSON): %s (slug: %s)\n", s.Title, slug)
	}

	if len(series) == 0 {
		return nil, fmt.Errorf("no series found in topic page")
	}

	return series, nil
}

// Helper function to clean series slugs
func cleanSeriesSlug(slug string) string {
	// Remove any number of "series/" prefixes
	for strings.HasPrefix(slug, "series/") {
		slug = strings.TrimPrefix(slug, "series/")
	}
	// Add back a single "series/" prefix
	return fmt.Sprintf("series/%s", slug)
}

func (d *Downloader) DownloadSeries(seriesSlug string) error {
	printBox(fmt.Sprintf("Downloading series: %s", seriesSlug))

	// Clean up the series slug by removing any "series/" prefixes
	cleanSlug := strings.TrimPrefix(seriesSlug, "series/")
	cleanSlug = strings.TrimPrefix(cleanSlug, "series/") // Remove second "series/" if present

	// For API requests, ensure we have the series/ prefix
	apiSlug := fmt.Sprintf("series/%s", cleanSlug)

	// Try to get series metadata from cache
	var seriesData SeriesMetadata
	cacheKey := fmt.Sprintf("series_%s", cleanSlug)

	found, err := d.Cache.Get(cacheKey, &seriesData)
	if err != nil {
		fmt.Printf("Cache error: %v, fetching fresh data\n", err)
		found = false
	}

	// Fetch fresh data if not found in cache or stale
	if !found || d.Cache.IsStale(cacheKey, 3600*24*7) {
		fmt.Println("Fetching series metadata from Laracasts...")

		// Use full series URL for API request
		seriesURL := fmt.Sprintf("%s/%s", config.LaracastsBaseUrl, apiSlug)
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
	outputDir := filepath.Join(d.BasePath, cleanSlug) // Modified line
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
		episode   Episode
		outputDir string
		err       error
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

				err := d.downloadEpisode(outputDir, episode)
				time.Sleep(time.Millisecond)
				results <- struct {
					episode   Episode
					outputDir string

					err error
				}{episode, outputDir, err}

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
			if err := d.saveDownloadState(cleanSlug, state); err != nil {
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

func (d *Downloader) DownloadAllSeries() error {
	printBox("Downloading all series")

	// Get the series listing page
	seriesURL := fmt.Sprintf("%s/series", config.LaracastsBaseUrl)

	req, err := http.NewRequest("GET", seriesURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			printBox("Failed to close response body")
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Try to extract data from data-page attribute first
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	var pageData string

	if matches := dataPageRe.FindSubmatch(body); len(matches) > 1 {
		pageData = html.UnescapeString(string(matches[1]))
	} else {
		// Fallback to script tag
		scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
		if matches := scriptRe.FindSubmatch(body); len(matches) > 1 {
			pageData = html.UnescapeString(string(matches[1]))
		}
	}

	if pageData == "" {
		return fmt.Errorf("no series data found in page")
	}

	// Parse the JSON structure
	var jsonData struct {
		Props struct {
			PublicCollections []struct {
				Items []struct {
					Slug string `json:"slug"`
				} `json:"items"`
			} `json:"publicCollections"`
			FeaturedCollection struct {
				Items []struct {
					Slug string `json:"slug"`
				} `json:"items"`
			} `json:"featuredCollection"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(pageData), &jsonData); err != nil {
		return fmt.Errorf("failed to parse JSON data: %v", err)
	}

	// Collect unique slugs and add "series/" prefix
	slugMap := make(map[string]bool)
	var slugs []string

	// Add featured collection slugs
	for _, item := range jsonData.Props.FeaturedCollection.Items {
		if item.Slug != "" && !slugMap[item.Slug] {
			slugMap[item.Slug] = true
			// Add "series/" prefix to match working format
			cleanSlug := cleanSeriesSlug(item.Slug)

			slugs = append(slugs, cleanSlug)
		}
	}

	// Add public collections slugs
	for _, collection := range jsonData.Props.PublicCollections {
		for _, item := range collection.Items {
			if item.Slug != "" && !slugMap[item.Slug] {
				slugMap[item.Slug] = true
				// Add "series/" prefix to match working format
				slugs = append(slugs, fmt.Sprintf("series/%s", item.Slug))
			}
		}
	}

	if len(slugs) == 0 {
		return fmt.Errorf("no series slugs found in page data")
	}

	fmt.Printf("\nFound %d series to download\n", len(slugs))
	for i, slug := range slugs {
		fmt.Printf("%d. %s\n", i+1, slug)
	}

	// Create channels for concurrent downloads
	sem := make(chan bool, 6) // Limit concurrent downloads
	var wg sync.WaitGroup
	var (
		completedSeries int32
		failedSeries    int32
		mu              sync.Mutex
	)

	// Process each series
	for i, slug := range slugs {
		wg.Add(1)
		sem <- true // Acquire semaphore

		go func(idx int, seriesSlug string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			mu.Lock()
			fmt.Printf("\n[%d/%d] 📺 Starting series: %s\n", idx+1, len(slugs), seriesSlug)
			mu.Unlock()

			// Use existing DownloadSeries function with full path
			if err := d.DownloadSeries(seriesSlug); err != nil {
				mu.Lock()
				fmt.Printf("❌ Error downloading series '%s': %v\n", seriesSlug, err)
				mu.Unlock()
				atomic.AddInt32(&failedSeries, 1)
				return
			}

			atomic.AddInt32(&completedSeries, 1)
			mu.Lock()
			fmt.Printf("✅ Completed series: %s\n", seriesSlug)

			progress := fmt.Sprintf("\nProgress: %.1f%% (%d/%d) Series Completed\n",
				float64(atomic.LoadInt32(&completedSeries))/float64(len(slugs))*100,
				atomic.LoadInt32(&completedSeries),
				len(slugs))
			fmt.Print(progress)
			mu.Unlock()

			// Small delay between series
			time.Sleep(500 * time.Millisecond)
		}(i, slug)
	}

	// Wait for all downloads to complete
	wg.Wait()

	// Print summary
	completed := atomic.LoadInt32(&completedSeries)
	failed := atomic.LoadInt32(&failedSeries)

	fmt.Printf("\n🎉 Download Summary:\n")
	fmt.Printf("Total Series Found: %d\n", len(slugs))
	fmt.Printf("Series Completed: %d\n", completed)
	fmt.Printf("Series Failed: %d\n", failed)

	if failed > 0 {
		return fmt.Errorf("%d series failed to download", failed)
	}

	return nil
}

func (d *Downloader) getSeriesPage() ([]struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}, string, error) {
	seriesURL := fmt.Sprintf("%s%s", config.LaracastsBaseUrl, config.LaracastsSeriesPath)
	fmt.Printf("Fetching series list from: %s\n", seriesURL)

	req, err := http.NewRequest("GET", seriesURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			print("Failed to close response body")
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %v", err)
	}

	// First try to find the data-page attribute
	dataPageRe := regexp.MustCompile(`data-page="([^"]+)"`)
	var pageData string

	if matches := dataPageRe.FindSubmatch(body); len(matches) > 1 {
		pageData = html.UnescapeString(string(matches[1]))
	} else {
		// Try finding the script tag with page data
		scriptRe := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
		if matches := scriptRe.FindSubmatch(body); len(matches) > 1 {
			pageData = html.UnescapeString(string(matches[1]))
		}
	}

	if pageData == "" {
		// Save the response for debugging
		debugFile := "debug_series_page.html"
		if err := os.WriteFile(debugFile, body, 0644); err == nil {
			fmt.Printf("Saved HTML content to %s for debugging\n", debugFile)
		}
		return nil, "", fmt.Errorf("no series data found in page")
	}

	// Parse the JSON data
	var pageStruct struct {
		Props struct {
			PublicCollections []struct {
				Items []struct {
					Title string `json:"title"`
					Slug  string `json:"slug"`
				} `json:"items"`
			} `json:"publicCollections"`
			FeaturedCollection struct {
				Items []struct {
					Title string `json:"title"`
					Slug  string `json:"slug"`
				} `json:"items"`
			} `json:"featuredCollection"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(pageData), &pageStruct); err != nil {
		return nil, "", fmt.Errorf("failed to parse page data: %v", err)
	}

	// Collect all unique series
	seriesMap := make(map[string]struct {
		Title string
		Slug  string
	})

	// Add series from featured collection
	for _, item := range pageStruct.Props.FeaturedCollection.Items {
		if item.Slug != "" {
			seriesMap[item.Slug] = struct {
				Title string
				Slug  string
			}{
				Title: item.Title,
				Slug:  item.Slug,
			}
		}
	}

	// Add series from public collections
	for _, collection := range pageStruct.Props.PublicCollections {
		for _, item := range collection.Items {
			if item.Slug != "" {
				seriesMap[item.Slug] = struct {
					Title string
					Slug  string
				}{
					Title: item.Title,
					Slug:  item.Slug,
				}
			}
		}
	}

	// Convert map to slice
	var series []struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	for _, s := range seriesMap {
		series = append(series, struct {
			Title string `json:"title"`
			Slug  string `json:"slug"`
		}{
			Title: s.Title,
			Slug:  s.Slug,
		})
	}

	if len(series) == 0 {
		return nil, "", fmt.Errorf("no series found in page data")
	}

	fmt.Printf("\nFound %d unique series\n", len(series))
	for i, s := range series {
		fmt.Printf("%d. %s (%s)\n", i+1, s.Title, s.Slug)
	}

	return series, "", nil
}

// Helper function to get raw XSRF token
func (d *Downloader) getXSRFTokenRaw() string {
	laracastsURL, _ := url.Parse(config.LaracastsBaseUrl)
	cookies := d.Client.Jar.Cookies(laracastsURL)

	for _, cookie := range cookies {
		if cookie.Name == "XSRF-TOKEN" {
			return cookie.Value
		}
	}
	return ""
}

// Login Update the cookie handling function to handle the initial request
func (d *Downloader) Login(email, password string) error {
	printBox("Authenticating")

	// First visit the site to get cookies
	homeReq, err := http.NewRequest("GET", config.LaracastsBaseUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create home request: %v", err)
	}

	homeReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	homeReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	homeResp, err := d.Client.Do(homeReq)
	if err != nil {
		return fmt.Errorf("failed home request: %v", err)
	}
	err = homeResp.Body.Close()
	if err != nil {
		return err
	}

	// Get XSRF token
	token, err := d.getXSRFToken()
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
	req.Header.Set("X-XSRF-TOKEN", token)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", config.LaracastsBaseUrl)

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed login request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			print("failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("✓ Logged in as %s\n", email)
	return nil
}
