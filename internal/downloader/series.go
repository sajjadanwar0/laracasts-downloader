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
	_ "net/url"
	"strings"
	"sync/atomic"

	"os"
	"path/filepath"
	"regexp"
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

func (d *Downloader) DownloadAllByTopics() error {
	printBox("Downloading all series organized by topics")

	// Get the topics page
	topicsURL := fmt.Sprintf("%s%s", config.LaracastsBaseUrl, "/browse/all")

	req, err := http.NewRequest("GET", topicsURL, nil)
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
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Updated regex to match topic cards in the HTML
	re := regexp.MustCompile(`<div class="topic-card[^"]*">.*?<a[^>]+href="([^"]+)"[^>]*>.*?<h2[^>]*>([^<]+)</h2>.*?<div class="text-left text-3xs[^"]*">(\d+)\s+Series.*?</div>`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	var topics []struct {
		URL         string
		Title       string
		SeriesCount int
	}

	for _, match := range matches {
		if len(match) >= 4 {
			seriesCount := 0
			fmt.Sscanf(match[3], "%d", &seriesCount)

			topics = append(topics, struct {
				URL         string
				Title       string
				SeriesCount int
			}{
				URL:         match[1],
				Title:       strings.TrimSpace(match[2]),
				SeriesCount: seriesCount,
			})
		}
	}

	if len(topics) == 0 {
		// Save HTML for debugging
		debugFile := "debug_browse_page.html"
		if err := os.WriteFile(debugFile, body, 0644); err == nil {
			fmt.Printf("Saved HTML content to %s for debugging\n", debugFile)
		}
		return fmt.Errorf("no topics found in page")
	}

	fmt.Printf("\nFound %d topics to process:\n", len(topics))
	for i, topic := range topics {
		fmt.Printf("%d. %s (%d series)\n", i+1, topic.Title, topic.SeriesCount)
	}

	// Process each topic
	var wg sync.WaitGroup
	sem := make(chan bool, 4) // Limit concurrent topics
	var mu sync.Mutex
	var (
		completedTopics int32
		failedTopics    int32
	)

	for i, topic := range topics {
		wg.Add(1)
		sem <- true // Acquire semaphore

		go func(idx int, topic struct {
			URL         string
			Title       string
			SeriesCount int
		}) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			// Create topic directory
			topicDir := filepath.Join(d.BasePath, "topics", sanitizeFilename(topic.Title))
			if err := os.MkdirAll(topicDir, 0755); err != nil {
				mu.Lock()
				fmt.Printf("❌ Error creating directory for topic '%s': %v\n", topic.Title, err)
				mu.Unlock()
				atomic.AddInt32(&failedTopics, 1)
				return
			}

			mu.Lock()
			fmt.Printf("\n[%d/%d] 📚 Processing topic: %s\n", idx+1, len(topics), topic.Title)
			mu.Unlock()

			// Get series for this topic
			series, err := d.getTopicSeries(topic.URL)
			if err != nil {
				mu.Lock()
				fmt.Printf("❌ Error getting series for topic '%s': %v\n", topic.Title, err)
				mu.Unlock()
				atomic.AddInt32(&failedTopics, 1)
				return
			}

			// Create a summary file for the topic
			summaryPath := filepath.Join(topicDir, "summary.txt")
			summaryContent := fmt.Sprintf("Topic: %s\nTotal Series: %d\n\nSeries List:\n", topic.Title, len(series))

			// Download each series
			for j, s := range series {
				seriesDir := filepath.Join(topicDir, sanitizeFilename(s.Title))

				// Override BasePath temporarily for this series
				originalBasePath := d.BasePath
				d.BasePath = cleanSeriesSlug(seriesDir)

				if err := d.DownloadSeries(s.Slug); err != nil {
					mu.Lock()
					fmt.Printf("❌ Error downloading series '%s': %v\n", s.Title, err)
					mu.Unlock()

					// Add to summary
					summaryContent += fmt.Sprintf("%d. %s (Failed: %v)\n", j+1, s.Title, err)
					continue
				}

				// Restore original BasePath
				d.BasePath = originalBasePath

				// Add successful series to summary
				summaryContent += fmt.Sprintf("%d. %s (Downloaded)\n", j+1, s.Title)
			}

			// Write summary file
			if err := os.WriteFile(summaryPath, []byte(summaryContent), 0644); err != nil {
				mu.Lock()
				fmt.Printf("⚠️ Error writing summary for topic '%s': %v\n", topic.Title, err)
				mu.Unlock()
			}

			atomic.AddInt32(&completedTopics, 1)
			mu.Lock()
			fmt.Printf("✅ Completed topic: %s\n", topic.Title)
			fmt.Printf("\nProgress: %.1f%% (%d/%d) Topics Completed\n",
				float64(atomic.LoadInt32(&completedTopics))/float64(len(topics))*100,
				atomic.LoadInt32(&completedTopics),
				len(topics))
			mu.Unlock()

		}(i, topic)
	}

	wg.Wait()

	// Print summary
	completed := atomic.LoadInt32(&completedTopics)
	failed := atomic.LoadInt32(&failedTopics)

	fmt.Printf("\n🎉 Download Summary:\n")
	fmt.Printf("Total Topics Found: %d\n", len(topics))
	fmt.Printf("Topics Completed: %d\n", completed)
	fmt.Printf("Topics Failed: %d\n", failed)

	// Create a global summary file
	globalSummaryPath := filepath.Join(d.BasePath, "topics", "download_summary.txt")
	globalSummaryContent := fmt.Sprintf(`Download Summary
===============
Date: %s
Total Topics: %d
Completed Topics: %d
Failed Topics: %d
`, time.Now().Format("2006-01-02 15:04:05"), len(topics), completed, failed)

	if err := os.WriteFile(globalSummaryPath, []byte(globalSummaryContent), 0644); err != nil {
		fmt.Printf("⚠️ Error writing global summary: %v\n", err)
	}

	if failed > 0 {
		return fmt.Errorf("%d topics failed to process", failed)
	}

	return nil
}

// Helper for the getTopicSeries function
func (d *Downloader) getTopicSeries(topicURL string) ([]struct {
	Title string
	Slug  string
}, error) {
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Look for series cards in the HTML
	seriesRe := regexp.MustCompile(`<a[^>]+href="/series/([^"]+)"[^>]*>.*?<h2[^>]*>([^<]+)</h2>`)
	matches := seriesRe.FindAllStringSubmatch(string(body), -1)

	seriesMap := make(map[string]struct {
		Title string
		Slug  string
	})

	for _, match := range matches {
		if len(match) >= 3 {
			slug := match[1]
			title := html.UnescapeString(strings.TrimSpace(match[2]))

			// Ensure unique entries
			seriesMap[slug] = struct {
				Title string
				Slug  string
			}{
				Title: title,
				Slug:  fmt.Sprintf("series/%s", slug),
			}
		}
	}

	// Convert map to slice
	var series []struct {
		Title string
		Slug  string
	}

	for _, s := range seriesMap {
		series = append(series, s)
	}

	if len(series) == 0 {
		// Save HTML for debugging
		debugFile := "debug_topic_page.html"
		if err := os.WriteFile(debugFile, body, 0644); err == nil {
			fmt.Printf("Saved HTML content to %s for debugging\n", debugFile)
		}
		return nil, fmt.Errorf("no series found in topic page")
	}

	fmt.Printf("\nFound %d series in topic\n", len(series))
	for i, s := range series {
		fmt.Printf("%d. %s (%s)\n", i+1, s.Title, s.Slug)
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
	defer resp.Body.Close()

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

func (d *Downloader) getSeriesPage(page int) ([]struct {
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
	defer resp.Body.Close()

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// Update the cookie handling function to handle the initial request
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
	homeResp.Body.Close()

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("✓ Logged in as %s\n", email)
	return nil
}
