package main

import (
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/sajjadanwar0/laracasts-dl/internal/downloader"
	"os"
	"path/filepath"
)

func main() {
	// Define flags
	var seriesFlag string

	// Set up flags without default values
	flag.StringVar(&seriesFlag, "s", "", "Series slug (leave empty to download all series)")

	// Parse flags
	flag.Parse()

	// Get the executable directory
	ex, err := os.Executable()
	if err != nil {
		fmt.Printf("Error getting executable path: %v\n", err)
		os.Exit(1)
	}
	exPath := filepath.Dir(ex)

	// Try to load .env from different possible locations
	envPaths := []string{
		".env",                              // Current directory
		"../../.env",                        // Two levels up (from cmd/laracasts-dl to project root)
		filepath.Join(exPath, ".env"),       // Executable directory
		filepath.Join(exPath, "../../.env"), // Two levels up from executable
	}

	envLoaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			envLoaded = true
			break
		}
	}

	if !envLoaded {
		fmt.Println("Error: Could not find .env file in any of these locations:")
		for _, path := range envPaths {
			absPath, _ := filepath.Abs(path)
			fmt.Printf("- %s\n", absPath)
		}
		os.Exit(1)
	}

	email := os.Getenv("EMAIL")
	password := os.Getenv("PASSWORD")

	if email == "" || password == "" {
		fmt.Println("Please set EMAIL and PASSWORD in .env file")
		os.Exit(1)
	}

	// Rest of your main function...
	// Check if any content type flag was specified
	seriesSpecified := isFlagSpecified("s")

	if !seriesSpecified {
		fmt.Println("Please specify at least one content type to download:")
		fmt.Println("  -s [slug]     : Download series (optional: specific series slug)")
		os.Exit(1)
	}

	// Initialize downloader
	dl, err := downloader.New()
	if err != nil {
		fmt.Printf("Error creating downloader: %v\n", err)
		os.Exit(1)
	}

	// Login
	if err := dl.Login(email, password); err != nil {
		fmt.Printf("Error logging in: %v\n", err)
		os.Exit(1)
	}

	// Process series downloads
	//if seriesSpecified {
	if seriesFlag != "" {
		if err := dl.DownloadSeries(seriesFlag); err != nil {
			fmt.Printf("Error downloading series: %v\n", err)
		}
	}
	//else {
	//if err := dl.DownloadAllSeries(); err != nil {
	//fmt.Printf("Error downloading all series: %v\n", err)
	//}
	//}
	//}
}

// isFlagSpecified checks if a flag was specified on the command line
func isFlagSpecified(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
