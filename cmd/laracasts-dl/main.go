package main

import (
	"flag"
	"fmt"
	"github.com/sajjadanwar0/laracasts-dl/internal/config"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/sajjadanwar0/laracasts-dl/internal/downloader"
)

func loadEnv() error {
	// Get the executable path
	ex, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error getting executable path: %v", err)
	}
	exePath := filepath.Dir(ex)

	// Try multiple possible locations for .env
	envPaths := []string{
		".env",                               // Current directory
		"../../.env",                         // Two levels up (from cmd/laracasts-dl to project root)
		filepath.Join(exePath, ".env"),       // Executable directory
		filepath.Join(exePath, "../../.env"), // Two levels up from executable
	}

	var loaded bool
	var loadErr error

	for _, path := range envPaths {
		absPath, _ := filepath.Abs(path)
		if err := godotenv.Load(absPath); err == nil {
			loaded = true
			fmt.Printf("Loaded environment from: %s\n", absPath)
			break
		} else {
			loadErr = err
		}
	}

	if !loaded {
		return fmt.Errorf("could not find .env file, last error: %v", loadErr)
	}

	// Validate all required environment variables
	for _, env := range config.RequiredEnvVars {
		if os.Getenv(env) == "" {
			return fmt.Errorf("required environment variable %s is not set", env)
		}
	}

	// Validate video quality
	if !config.ValidateVideoQuality(os.Getenv("VIDEO_QUALITY")) {
		return fmt.Errorf("invalid VIDEO_QUALITY in .env. Must be one of: 360p, 540p, 720p, 1080p")
	}

	return nil
}
func main() {
	// Define flags
	var (
		seriesFlag string
		clearCache bool
		noCache    bool
		workers    int
		chunkSize  int
	)

	// Define flags but don't parse yet
	flag.StringVar(&seriesFlag, "s", "", "Series slug to download (leave empty to download all series)")
	flag.BoolVar(&clearCache, "clear-cache", false, "Clear the cache before starting")
	flag.BoolVar(&noCache, "no-cache", false, "Ignore cache and download fresh")
	flag.IntVar(&workers, "workers", 15, "Number of concurrent downloads (default: 15)")
	flag.IntVar(&chunkSize, "chunk-size", 20, "Chunk size in MB (default: 20)")

	// Parse flags
	flag.Parse()

	// Load environment variables
	if err := loadEnv(); err != nil {
		fmt.Printf("Error loading environment: %v\n", err)
		fmt.Println("Make sure .env file exists in the project root with EMAIL and PASSWORD")
		os.Exit(1)
	}

	email := os.Getenv("EMAIL")
	password := os.Getenv("PASSWORD")

	if email == "" || password == "" {
		fmt.Println("Please set EMAIL and PASSWORD in .env file")
		os.Exit(1)
	}

	// Initialize downloader
	dl, err := downloader.New()
	if err != nil {
		fmt.Printf("Error creating downloader: %v\n", err)
		os.Exit(1)
	}

	// Handle cache flags
	if clearCache {
		fmt.Println("Clearing cache...")
		if err := dl.Cache.Clear(); err != nil {
			fmt.Printf("Error clearing cache: %v\n", err)
			os.Exit(1)
		}
	}

	// Login to Laracasts
	if err := dl.Login(email, password); err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	// Check if -s flag was provided (regardless of value)
	isFlagProvided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "s" {
			isFlagProvided = true
		}
	})

	// Handle downloads based on flag state
	var downloadErr error
	if isFlagProvided && seriesFlag != "" {
		// Specific series download
		fmt.Printf("Downloading specific series: %s\n", seriesFlag)
		downloadErr = dl.DownloadSeries(seriesFlag)
	} else {
		// Download all series if:
		// 1. No -s flag was provided at all
		// 2. -s flag was provided but empty (-s "")
		fmt.Println("No series specified, downloading all series...")
		downloadErr = dl.DownloadAllSeries()
	}

	if downloadErr != nil {
		fmt.Printf("\nError during download: %v\n", downloadErr)
		os.Exit(1)
	}

	fmt.Println("\nDownload completed successfully!")
}
