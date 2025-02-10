// cmd/laracasts-dl/main.go

package main

import (
	"flag"
	"fmt"
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

	// Download series
	if seriesFlag != "" {
		// Download specific series
		if err := dl.DownloadSeries(seriesFlag); err != nil {
			fmt.Printf("\nError downloading series: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Download all series
		fmt.Println("\nNo series specified, downloading all series...")
		if err := dl.DownloadAllSeries(); err != nil {
			fmt.Printf("\nError downloading all series: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("\nDownload completed successfully!")
}
