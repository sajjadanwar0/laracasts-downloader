package vimeo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
	}
}

func (c *Client) GetVideoConfig(vimeoId string) (*VideoConfig, error) {
	configURL := fmt.Sprintf("https://player.vimeo.com/video/%s/config", vimeoId)
	maxRetries := MaxRetries
	var lastErr error

	headers := map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Accept":          "application/json",
		"Accept-Language": "en-US,en;q=0.9",
		"Referer":         "https://laracasts.com/",
		"Origin":          "https://laracasts.com",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "cross-site",
		"Connection":      "keep-alive",
	}

	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", configURL, nil)
		if err != nil {
			lastErr = err
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		err = resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			fmt.Printf("Response body: %s\n", string(body))
			time.Sleep(time.Second)
			continue
		}

		var config VideoConfig
		if err := json.Unmarshal(body, &config); err != nil {
			lastErr = err
			continue
		}

		// Debug output
		fmt.Printf("\nVideo formats found for %s:\n", vimeoId)
		fmt.Printf("Progressive: %d formats\n", len(config.Request.Files.Progressive))
		fmt.Printf("HLS: %v\n", config.Request.Files.HLS.DefaultCDN != "")
		fmt.Printf("DASH: %v\n", config.Request.Files.Dash.DefaultCDN != "")

		return &config, nil
	}

	return nil, fmt.Errorf("failed after %d attempts: %v", maxRetries, lastErr)
}
func (c *Client) DownloadVideo(config *VideoConfig, outputPath string) error {
	// Try progressive download first
	if len(config.Request.Files.Progressive) > 0 {
		fmt.Println("Available video formats:")
		var bestURL string
		var bestQuality int
		for _, prog := range config.Request.Files.Progressive {
			fmt.Printf("- Quality: %s, URL: available\n", prog.Quality)
			quality := 0
			_, err := fmt.Sscanf(prog.Quality, "%dp", &quality)
			if err != nil {
				return err
			}
			if quality > bestQuality {
				bestQuality = quality
				bestURL = prog.URL
			}
		}

		if bestURL != "" {
			fmt.Printf("\nDownloading progressive MP4 stream (%dp)\n", bestQuality)
			return c.downloadWithChunks(bestURL, outputPath)
		}
	}

	// Try HLS if progressive download is not available
	if config.Request.Files.HLS.DefaultCDN != "" {
		fmt.Println("\nTrying HLS stream...")
		if cdn, ok := config.Request.Files.HLS.Cdns[config.Request.Files.HLS.DefaultCDN]; ok {
			hlsURL := cdn.URL
			if hlsURL != "" {
				return c.downloadHLSVideo(hlsURL, outputPath)
			}
		}
		fmt.Printf("Available CDNs: %v\n", config.Request.Files.HLS.Cdns)
	}

	// Try Dash stream if available
	if config.Request.Files.Dash.DefaultCDN != "" {
		fmt.Println("\nTrying DASH stream...")
		if cdn, ok := config.Request.Files.Dash.Cdns[config.Request.Files.Dash.DefaultCDN]; ok {
			dashURL := cdn.URL
			if dashURL != "" {
				return c.downloadDashVideo(dashURL, outputPath)
			}
		}
	}

	return fmt.Errorf("no suitable video URL found (tried Progressive, HLS, and DASH)")
}

func (c *Client) downloadDashVideo(url, outputPath string) error {
	fmt.Printf("Downloading DASH stream: %s\n", filepath.Base(outputPath))

	cmd := exec.Command("ffmpeg",
		"-i", url,
		"-c", "copy",
		"-movflags", "+faststart",
		"-y",
		outputPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %v\nOutput: %s", err, stderr.String())
	}

	return nil
}

func (c *Client) downloadHLSVideo(url, outputPath string) error {
	fmt.Printf("Downloading HLS stream: %s\n", filepath.Base(outputPath))

	cmd := exec.Command("ffmpeg",
		"-i", url,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		"-y",
		outputPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %v\nOutput: %s", err, stderr.String())
	}

	return nil
}

func (c *Client) getBestProgressiveURL(config *VideoConfig) (string, int) {
	var bestURL string
	var bestQuality int

	for _, prog := range config.Request.Files.Progressive {
		quality := 0
		_, err := fmt.Sscanf(prog.Quality, "%dp", &quality)
		if err != nil {
			return "", 0
		}
		if quality > bestQuality {
			bestQuality = quality
			bestURL = prog.URL
		}
	}

	return bestURL, bestQuality
}

func (c *Client) downloadWithChunks(url string, outputPath string) error {
	// Get file size
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HEAD request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://laracasts.com/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed HEAD request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			print("Failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HEAD request failed with status: %d", resp.StatusCode)
	}

	fileSize := resp.ContentLength
	if fileSize <= 0 {
		return fmt.Errorf("invalid file size: %d", fileSize)
	}

	// Create buffered file writer
	writer, err := NewBufferedFileWriter(outputPath, fileSize)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer func(writer *BufferedFileWriter) {
		err := writer.Close()
		if err != nil {
			print("Failed to close output file")
		}
	}(writer)

	// Setup progress bar
	bar := progressbar.NewOptions64(
		fileSize,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	// Calculate chunks
	numChunks := int(math.Ceil(float64(fileSize) / float64(ChunkSize)))
	chunks := make([]struct {
		start int64
		end   int64
	}, numChunks)

	for i := 0; i < numChunks; i++ {
		start := int64(i) * ChunkSize
		end := start + ChunkSize
		if end > fileSize {
			end = fileSize
		}
		chunks[i] = struct {
			start int64
			end   int64
		}{start, end}
	}

	// Create buffer pool
	bufferPool := sync.Pool{
		New: func() interface{} {
			return make([]byte, MemoryBuffer)
		},
	}

	// Download chunks
	var wg sync.WaitGroup
	errors := make(chan error, numChunks)
	limiter := make(chan struct{}, MaxChunkWorkers)

	for i, chunk := range chunks {
		wg.Add(1)
		go func(chunkIndex int, start, end int64) {
			defer wg.Done()
			limiter <- struct{}{}        // Acquire semaphore
			defer func() { <-limiter }() // Release semaphore

			// Get buffer from pool
			buffer := bufferPool.Get().([]byte)
			defer bufferPool.Put(buffer)

			// Retry logic for chunk download
			var lastErr error
			for retry := 0; retry < MaxRetries; retry++ {
				if err := c.downloadChunk(url, writer, start, end, bar, buffer); err != nil {
					lastErr = err
					time.Sleep(time.Second)
					continue
				}
				lastErr = nil
				break
			}

			if lastErr != nil {
				errors <- fmt.Errorf("chunk %d failed after %d retries: %v",
					chunkIndex, MaxRetries, lastErr)
			}
		}(i, chunk.start, chunk.end)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errMsgs []string
	for err := range errors {
		if err != nil {
			errMsgs = append(errMsgs, err.Error())
		}
	}

	if len(errMsgs) > 0 {
		return fmt.Errorf("chunk download errors:\n%s",
			strings.Join(errMsgs, "\n"))
	}

	fmt.Println() // New line after progress bar
	return nil
}

func (c *Client) downloadChunk(url string, writer *BufferedFileWriter,
	start, end int64, bar *progressbar.ProgressBar, buffer []byte) error {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end-1))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://laracasts.com/")
	req.Header.Set("Origin", "https://laracasts.com")
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("chunk request failed: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			print("Failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read and write chunk using buffer
	reader := bufio.NewReader(resp.Body)
	written := int64(0)

	for written < end-start {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read chunk: %v", err)
		}
		if n == 0 {
			break
		}

		if _, err := writer.WriteAt(buffer[:n], start+written); err != nil {
			return fmt.Errorf("failed to write chunk: %v", err)
		}

		written += int64(n)
		err = bar.Add64(int64(n))
		if err != nil {
			return err
		}
	}

	return nil
}
