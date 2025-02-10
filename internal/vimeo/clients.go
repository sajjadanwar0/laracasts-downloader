package vimeo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/schollz/progressbar/v3"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client handles Vimeo video downloads
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new Vimeo client
func NewClient(httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
	}
}

// VideoConfig represents the Vimeo video configuration
type VideoConfig struct {
	Request struct {
		Files struct {
			Progressive []struct {
				URL     string `json:"url"`
				Quality string `json:"quality"`
			} `json:"progressive"`
			HLS struct {
				DefaultCDN string `json:"default_cdn"`
				Cdns       map[string]struct {
					URL string `json:"url"`
				} `json:"cdns"`
			} `json:"hls"`
		} `json:"files"`
	} `json:"request"`
}

// GetVideoConfig fetches the video configuration from Vimeo
func (c *Client) GetVideoConfig(vimeoId string) (*VideoConfig, error) {
	configURL := fmt.Sprintf("https://player.vimeo.com/video/%s/config", vimeoId)
	maxRetries := 3
	var lastErr error

	headers := map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Accept":          "application/json",
		"Accept-Language": "en-US,en;q=0.9",
		"Referer":         "https://laracasts.com/",
		"Origin":          "https://laracasts.com",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "cross-site",
	}

	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", configURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %v", err)
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
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
			continue
		}

		// Check if response is HTML (error page)
		if strings.Contains(string(body), "<html") {
			lastErr = fmt.Errorf("received HTML instead of JSON response")
			continue
		}

		var config VideoConfig
		if err := json.Unmarshal(body, &config); err != nil {
			lastErr = err
			continue
		}

		return &config, nil
	}

	return nil, fmt.Errorf("failed after %d attempts: %v", maxRetries, lastErr)
}

// DownloadVideo downloads the video using the best available method
func (c *Client) DownloadVideo(config *VideoConfig, outputPath string) error {
	// Try progressive download first
	if len(config.Request.Files.Progressive) > 0 {
		var bestURL string
		var bestQuality int
		for _, prog := range config.Request.Files.Progressive {
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
			fmt.Printf("Downloading progressive stream (%dp)\n", bestQuality)
			if err := c.downloadProgressiveVideo(bestURL, outputPath); err == nil {
				return nil
			}
		}
	}

	// Try HLS if progressive download is not available or failed
	if config.Request.Files.HLS.DefaultCDN != "" {
		defaultCDN := config.Request.Files.HLS.DefaultCDN
		if cdn, ok := config.Request.Files.HLS.Cdns[defaultCDN]; ok {
			hlsURL := cdn.URL
			if hlsURL != "" {
				fmt.Printf("Downloading HLS stream\n")
				return c.downloadWithFFmpeg(hlsURL, outputPath)
			}
		}
	}

	return fmt.Errorf("no suitable video URL found")
}

// downloadProgressiveVideo downloads a direct video stream
func (c *Client) downloadProgressiveVideo(url, outputPath string) error {
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			err := resp.Body.Close()
			if err != nil {
				return err
			}

			lastErr = fmt.Errorf("bad status: %s", resp.Status)
			continue
		}

		out, err := os.Create(outputPath)
		if err != nil {
			err := resp.Body.Close()
			if err != nil {
				return err
			}
			return fmt.Errorf("failed to create output file: %v", err)
		}

		bar := progressbar.NewOptions64(
			resp.ContentLength,
			progressbar.OptionSetDescription("Downloading"),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(30),
			progressbar.OptionShowCount(),
		)

		_, err = io.Copy(io.MultiWriter(out, bar), resp.Body)
		err = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		err = out.Close()
		if err != nil {
			return err
		}

		fmt.Println() // New line after progress bar
		return nil
	}

	return fmt.Errorf("download failed after %d attempts: %v", maxRetries, lastErr)
}

// downloadWithFFmpeg downloads a video using FFmpeg
func (c *Client) downloadWithFFmpeg(url, outputPath string) error {
	fmt.Printf("Using FFmpeg to download: %s\n", url)

	cmd := exec.Command("ffmpeg",
		"-i", url,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-y",
		outputPath,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	var barMutex sync.Mutex
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionSetWriter(os.Stdout),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionSetRenderBlankState(true),
	)

	durationFound := make(chan time.Duration, 1)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Duration:") {
				parts := strings.Split(line, ",")
				if len(parts) > 0 {
					durStr := strings.TrimPrefix(parts[0], "Duration:")
					durStr = strings.TrimSpace(durStr)
					if duration, err := time.ParseDuration(strings.Replace(durStr, ":", "h", 1) + "m"); err == nil {
						durationFound <- duration
						break
					}
				}
			}
		}
	}()

	var totalDuration time.Duration
	progressRegex := regexp.MustCompile(`out_time_ms=(\d+)`)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()

			select {
			case duration := <-durationFound:
				totalDuration = duration
				barMutex.Lock()
				bar.ChangeMax64(int64(totalDuration.Seconds()))
				barMutex.Unlock()
			default:
			}

			matches := progressRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				microsecondsStr := matches[1]
				microseconds, err := strconv.ParseInt(microsecondsStr, 10, 64)
				if err == nil {
					seconds := microseconds / 1000000
					barMutex.Lock()
					err := bar.Set64(seconds)
					if err != nil {
						return
					}
					barMutex.Unlock()
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %v", err)
	}

	barMutex.Lock()
	err = bar.Finish()
	if err != nil {
		return err
	}
	barMutex.Unlock()
	fmt.Println()

	if info, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("failed to verify download: %v", err)
	} else if info.Size() < 1024*1024 {
		return fmt.Errorf("downloaded file is too small (%d bytes)", info.Size())
	}

	return nil
}
