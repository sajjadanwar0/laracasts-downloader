// bits.go

package downloader

import (
	"encoding/json"
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Bit struct {
	Title   string
	VimeoId string
	Path    string
	Series  struct {
		Title string
	}
	Author struct {
		Username string
	}
	LengthForHumans string
}

type BitsDownloadState struct {
	Completed map[string]bool `json:"completed"`
	LastSync  time.Time       `json:"last_sync"`
}

func (d *Downloader) loadBitsDownloadState() (*BitsDownloadState, error) {
	var state BitsDownloadState
	found, err := d.Cache.Get("bits_download_state", &state)
	if err != nil || !found {
		return &BitsDownloadState{
			Completed: make(map[string]bool),
			LastSync:  time.Now(),
		}, nil
	}
	return &state, nil
}

func (d *Downloader) saveBitsDownloadState(state *BitsDownloadState) error {
	state.LastSync = time.Now()
	return d.Cache.Set("bits_download_state", state)
}

func (d *Downloader) DownloadAllBits() error {
	printBox("Downloading all Laracasts Bits")

	// Create bits directory in the base path
	bitsDir := filepath.Join(d.BasePath, "bits")
	if err := os.MkdirAll(bitsDir, 0755); err != nil {
		return fmt.Errorf("failed to create bits directory: %v", err)
	}

	// Get all bits
	bits, err := d.fetchBits()
	if err != nil {
		return fmt.Errorf("failed to fetch bits: %v", err)
	}

	fmt.Printf("\nFound %d bits to download\n", len(bits))

	// Load download state
	state, err := d.loadBitsDownloadState()
	if err != nil {
		fmt.Printf("Warning: Failed to load download state: %v\n", err)
	}

	// Count already downloaded bits
	var alreadyDownloaded int
	for _, bit := range bits {
		if state.Completed[bit.Path] {
			alreadyDownloaded++
		}
	}

	fmt.Printf("Already downloaded: %d bits\n", alreadyDownloaded)
	fmt.Printf("Remaining to download: %d bits\n", len(bits)-alreadyDownloaded)

	// Create worker pool for concurrent downloads
	sem := make(chan bool, MaxEpisodeWorkers)
	var wg sync.WaitGroup
	var (
		completedBits int32
		failedBits    int32
		mu            sync.Mutex
	)

	// Process each bit
	for i, bit := range bits {
		// Skip if already downloaded (from cache)
		if state.Completed[bit.Path] {
			continue
		}

		wg.Add(1)
		sem <- true // Acquire semaphore

		go func(idx int, bit Bit) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			mu.Lock()
			fmt.Printf("\n[%d/%d] 📹 Starting bit: %s\n", idx+1, len(bits), bit.Title)
			mu.Unlock()

			if err := d.downloadBit(bitsDir, bit); err != nil {
				mu.Lock()
				fmt.Printf("❌ Error downloading bit '%s': %v\n", bit.Title, err)
				mu.Unlock()
				atomic.AddInt32(&failedBits, 1)
				return
			}

			atomic.AddInt32(&completedBits, 1)
			mu.Lock()
			fmt.Printf("✅ Completed bit: %s\n", bit.Title)
			progress := fmt.Sprintf("\nProgress: %.1f%% (%d/%d) Bits Completed\n",
				float64(atomic.LoadInt32(&completedBits))/float64(len(bits)-alreadyDownloaded)*100,
				atomic.LoadInt32(&completedBits),
				len(bits)-alreadyDownloaded)
			fmt.Print(progress)
			mu.Unlock()

			// Small delay between downloads
			time.Sleep(500 * time.Millisecond)
		}(i, bit)
	}

	// Wait for all downloads to complete
	wg.Wait()

	// Print summary
	completed := atomic.LoadInt32(&completedBits)
	failed := atomic.LoadInt32(&failedBits)

	fmt.Printf("\n🎉 Download Summary:\n")
	fmt.Printf("Total Bits Found: %d\n", len(bits))
	fmt.Printf("Previously Downloaded: %d\n", alreadyDownloaded)
	fmt.Printf("Newly Downloaded: %d\n", completed)
	fmt.Printf("Failed Downloads: %d\n", failed)

	if failed > 0 {
		return fmt.Errorf("%d bits failed to download", failed)
	}

	return nil
}

// fetchBits retrieves all bits from all pages
func (d *Downloader) fetchBits() ([]Bit, error) {
	var allBits []Bit
	page := 1
	maxPages := 1
	hasMore := true

	fmt.Println("Starting to fetch all bits...")

	for hasMore {
		fmt.Printf("\nFetching page %d...\n", page)
		bits, totalPages, err := d.fetchBitsPage(page)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %v", page, err)
		}

		if maxPages == 1 {
			maxPages = totalPages
			fmt.Printf("Found %d total pages\n", maxPages)
		}

		allBits = append(allBits, bits...)
		fmt.Printf("Found %d bits on page %d\n", len(bits), page)

		page++
		hasMore = page <= maxPages

		if hasMore {
			// Add a small delay between requests
			time.Sleep(500 * time.Millisecond)
		}
	}

	fmt.Printf("\nTotal bits found: %d\n", len(allBits))
	return allBits, nil
}

func (d *Downloader) fetchBitsPage(page int) ([]Bit, int, error) {
	bitsURL := fmt.Sprintf("%s%s", config.LaracastsBaseUrl, config.LaracastsBitsPath)
	if page > 1 {
		bitsURL = fmt.Sprintf("%s?page=%d", bitsURL, page)
	}

	fmt.Printf("Fetching from URL: %s\n", bitsURL)

	req, err := http.NewRequest("GET", bitsURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %v", err)
	}

	for k, v := range config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %v", err)
	}

	// Save raw response for debugging
	err = os.WriteFile("bits_response.html", body, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to save debug file: %v\n", err)
	}

	// Extract JSON data from script tag
	scriptPattern := regexp.MustCompile(`<script\s+id="page-data"\s+type="application/json"[^>]*>(.*?)</script>`)
	matches := scriptPattern.FindSubmatch(body)

	var jsonData string
	if len(matches) > 1 {
		jsonData = html.UnescapeString(string(matches[1]))
	} else {
		// Try alternative data-page attribute
		dataPagePattern := regexp.MustCompile(`data-page="([^"]+)"`)
		matches = dataPagePattern.FindSubmatch(body)
		if len(matches) > 1 {
			jsonData = html.UnescapeString(string(matches[1]))
		}
	}

	if jsonData == "" {
		return nil, 0, fmt.Errorf("could not find page data")
	}

	// Save extracted JSON for debugging
	err = os.WriteFile("bits_data_extracted.json", []byte(jsonData), 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to save debug JSON: %v\n", err)
	}

	var pageData struct {
		Props struct {
			Bits []struct {
				ID      int    `json:"id"`
				Title   string `json:"title"`
				VimeoId string `json:"vimeoId"`
				Path    string `json:"path"`
				Series  struct {
					Title string `json:"title"`
				} `json:"series"`
				Author struct {
					Username string `json:"username"`
				} `json:"author"`
				LengthForHumans string `json:"lengthForHumans"`
			} `json:"bits"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonData), &pageData); err != nil {
		return nil, 0, fmt.Errorf("failed to parse JSON data: %v, JSON: %s", err, jsonData)
	}

	var bits []Bit
	for _, rawBit := range pageData.Props.Bits {
		bit := Bit{
			Title:           rawBit.Title,
			VimeoId:         rawBit.VimeoId,
			Path:            rawBit.Path,
			Series:          struct{ Title string }(rawBit.Series),
			Author:          struct{ Username string }(rawBit.Author),
			LengthForHumans: rawBit.LengthForHumans,
		}
		bits = append(bits, bit)
		fmt.Printf("Found bit: %s by %s (%s)\n", bit.Title, bit.Author.Username, bit.LengthForHumans)
	}

	return bits, 1, nil // Just one page since pagination info isn't in the JSON
}

func (d *Downloader) fetchBitDetails(bit *Bit) error {
	// Clean up the episode path if needed
	episodePath := bit.Path
	if !strings.HasPrefix(episodePath, "/episodes/") {
		episodePath = fmt.Sprintf("/episodes/%s", strings.TrimPrefix(episodePath, "/"))
	}

	episodeURL := fmt.Sprintf("%s%s", config.LaracastsBaseUrl, episodePath)
	fmt.Printf("\nFetching details from: %s\n", episodeURL)

	// Create new request
	req, err := http.NewRequest("GET", episodeURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Add all necessary headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", config.LaracastsBaseUrl)
	req.Header.Set("Cache-Control", "no-cache")

	// Get XSRF token if available
	token := d.getXSRFTokenRaw()
	if token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}

	// Make the request with retries
	var resp *http.Response
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		resp, err = d.Client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(time.Second * time.Duration(i+1))
	}

	if err != nil {
		return fmt.Errorf("failed all request attempts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Save response for debugging
	debugFile := fmt.Sprintf("debug_episode_%s.html", strings.TrimPrefix(episodePath, "/episodes/"))
	if err := os.WriteFile(debugFile, body, 0644); err != nil {
		fmt.Printf("Warning: Failed to save debug file: %v\n", err)
	}

	// Try to find vimeoId in the page content
	bodyStr := string(body)

	// First try: Extract from script tag with page data
	scriptPattern := regexp.MustCompile(`<script[^>]*?id="page-data"[^>]*?>(.*?)</script>`)
	if matches := scriptPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		jsonData := html.UnescapeString(matches[1])
		if vimeoId := extractVimeoIdFromJSON(jsonData); vimeoId != "" {
			bit.VimeoId = vimeoId
			return nil
		}
	}

	// Second try: Look for data-page attribute
	dataPagePattern := regexp.MustCompile(`data-page="([^"]+)"`)
	if matches := dataPagePattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		jsonData := html.UnescapeString(matches[1])
		if vimeoId := extractVimeoIdFromJSON(jsonData); vimeoId != "" {
			bit.VimeoId = vimeoId
			return nil
		}
	}

	// Third try: Direct search for vimeoId
	vimeoPattern := regexp.MustCompile(`"vimeoId"\s*:\s*"([^"]+)"`)
	if matches := vimeoPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		bit.VimeoId = matches[1]
		return nil
	}

	return fmt.Errorf("could not find VimeoId in page")
}

func extractVimeoIdFromJSON(jsonData string) string {
	var pageData struct {
		Props struct {
			Episode struct {
				VimeoId string `json:"vimeoId"`
			} `json:"episode"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonData), &pageData); err == nil {
		return pageData.Props.Episode.VimeoId
	}

	// Try alternate structure
	var altData struct {
		Component string `json:"component"`
		Props     struct {
			VimeoId string `json:"vimeoId"`
		} `json:"props"`
	}

	if err := json.Unmarshal([]byte(jsonData), &altData); err == nil {
		return altData.Props.VimeoId
	}

	return ""
}

func (d *Downloader) downloadBit(bitsDir string, bit Bit) error {
	// Load download state
	state, err := d.loadBitsDownloadState()
	if err != nil {
		fmt.Printf("Warning: Failed to load download state: %v\n", err)
	}

	// Check if bit is already downloaded in cache
	if state.Completed[bit.Path] {
		fmt.Printf("Bit already downloaded (from cache): %s\n", bit.Title)
		return nil
	}

	// Create series subdirectory if we have series info
	var outputDir string
	if bit.Series.Title != "" {
		seriesDir := sanitizeFilename(bit.Series.Title)
		outputDir = filepath.Join(bitsDir, seriesDir)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create series directory: %v", err)
		}
	} else {
		outputDir = bitsDir
	}

	// Create filename with just title and duration
	filename := sanitizeFilename(bit.Title)

	if bit.LengthForHumans != "" {
		filename += fmt.Sprintf(" (%s)", bit.LengthForHumans)
	}
	filename += ".mp4"

	outputPath := filepath.Join(outputDir, filename)

	// Check if file already exists on disk
	if info, err := os.Stat(outputPath); err == nil && info.Size() > 0 {
		fmt.Printf("Bit already downloaded (from disk): %s\n", filename)
		// Update cache state
		state.Completed[bit.Path] = true
		if err := d.saveBitsDownloadState(state); err != nil {
			fmt.Printf("Warning: Failed to save download state: %v\n", err)
		}
		return nil
	}

	fmt.Printf("\nDownloading bit: %s\n", filename)
	fmt.Printf("Using VimeoId: %s\n", bit.VimeoId)

	// Get video configuration
	videoConfig, err := d.Vimeo.GetVideoConfig(bit.VimeoId)
	if err != nil {
		return fmt.Errorf("failed to get video config: %v", err)
	}

	// Download the video
	if err := d.Vimeo.DownloadVideo(videoConfig, outputPath); err != nil {
		return err
	}

	// Update cache state after successful download
	state.Completed[bit.Path] = true
	if err := d.saveBitsDownloadState(state); err != nil {
		fmt.Printf("Warning: Failed to save download state: %v\n", err)
	}

	return nil
}
