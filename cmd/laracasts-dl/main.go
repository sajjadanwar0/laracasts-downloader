package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/sajjadnwar0/laracasts-dl/internal/downloader"
	"github.com/sajjanwar0/laracasts-dl/internal/config"
)

func main() {
	// Define flags for different content types
	seriesFlag := flag.String("s", "", "Series slug (leave empty to download all series)")
	larabitFlag := flag.String("l", "", "Larabit slug (leave empty to download all larabits)")
	topicFlag := flag.String("t", "", "Topic slug (leave empty to download all topics)")
	teacherFlag := flag.String("teacher", "", "Teacher name for filtering bits (e.g., JeffreyWay)")
	flag.Parse()

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Error loading .env file: %v\n", err)
		os.Exit(1)
	}

	email := os.Getenv("EMAIL")
	password := os.Getenv("PASSWORD")

	if email == "" || password == "" {
		fmt.Println("Please set EMAIL and PASSWORD in .env file")
		os.Exit(1)
	}

	// No flags provided
	if *seriesFlag == "" && *larabitFlag == "" && *topicFlag == "" {
		fmt.Println("Please provide at least one flag:")
		fmt.Println("  -s [slug]     : Download series (optional: specific series slug)")
		fmt.Println("  -l [slug]     : Download larabits (optional: specific larabit slug)")
		fmt.Println("  -t [slug]     : Download topics (optional: specific topic slug)")
		fmt.Println("  -teacher name : Filter bits by teacher (e.g., -l -teacher JeffreyWay)")
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

	// Process flags
	if flag.Lookup("s").Value.String() != "" {
		contentType := config.ContentTypes["s"]
		if *seriesFlag != "" {
			if err := dl.DownloadSeries(*seriesFlag); err != nil {
				fmt.Printf("Error downloading series: %v\n", err)
			}
		} else {
			if err := dl.DownloadAllSeries(); err != nil {
				fmt.Printf("Error downloading all series: %v\n", err)
			}
		}
	}

	if flag.Lookup("l").Value.String() != "" {
		contentType := config.ContentTypes["l"]
		if *larabitFlag != "" {
			if err := dl.DownloadBit(*larabitFlag); err != nil {
				fmt.Printf("Error downloading larabit: %v\n", err)
			}
		} else {
			if err := dl.DownloadAllBits(*teacherFlag); err != nil {
				fmt.Printf("Error downloading larabits: %v\n", err)
			}
		}
	}

	if flag.Lookup("t").Value.String() != "" {
		contentType := config.ContentTypes["t"]
		if *topicFlag != "" {
			if err := dl.DownloadTopic(*topicFlag); err != nil {
				fmt.Printf("Error downloading topic: %v\n", err)
			}
		} else {
			if err := dl.DownloadAllTopics(); err != nil {
				fmt.Printf("Error downloading all topics: %v\n", err)
			}
		}
	}
}
