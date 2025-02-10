package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CacheEntry struct {
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

type Cache struct {
	BasePath string
	mutex    sync.RWMutex
}

func NewCache(basePath string) (*Cache, error) {
	cachePath := filepath.Join(basePath, ".cache")
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	for _, dir := range []string{"series", "downloads", "state"} {
		dirPath := filepath.Join(cachePath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create cache subdirectory %s: %v", dir, err)
		}
	}

	cache := &Cache{BasePath: cachePath}
	if err := cache.verifyDirectories(); err != nil {
		return nil, err
	}

	return cache, nil
}

func (c *Cache) verifyDirectories() error {
	dirs := []string{"", "series", "downloads", "state"}
	for _, dir := range dirs {
		path := filepath.Join(c.BasePath, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("cache directory not found: %s", path)
		}
	}
	return nil
}

func (c *Cache) Set(key string, data interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	key = strings.ReplaceAll(key, "/", "_")
	key = strings.ReplaceAll(key, "\\", "_")

	var subdir string
	switch {
	case strings.HasPrefix(key, "series_"):
		subdir = "series"
	case strings.HasPrefix(key, "download_"):
		subdir = "downloads"
	default:
		subdir = "state"
	}

	dirPath := filepath.Join(c.BasePath, subdir)
	filePath := filepath.Join(dirPath, key+".json")

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to ensure cache directory: %v", err)
	}

	entry := CacheEntry{
		Data:      data,
		Timestamp: time.Now(),
	}

	jsonData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %v", err)
	}

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %v", err)
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		err := os.Remove(tmpFile)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to save cache file: %v", err)
	}

	return nil
}

func (c *Cache) Get(key string, data interface{}) (bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	subdirs := []string{"series", "downloads", "state"}
	var filePath string
	var exists bool

	for _, subdir := range subdirs {
		path := filepath.Join(c.BasePath, subdir, key+".json")
		if _, err := os.Stat(path); err == nil {
			filePath = path
			exists = true
			break
		}
	}

	if !exists {
		return false, nil
	}

	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to read cache file: %v", err)
	}

	var entry CacheEntry
	if err := json.Unmarshal(jsonData, &entry); err != nil {
		return false, fmt.Errorf("failed to unmarshal cache entry: %v", err)
	}

	jsonData, err = json.Marshal(entry.Data)
	if err != nil {
		return false, fmt.Errorf("failed to marshal cached data: %v", err)
	}

	if err := json.Unmarshal(jsonData, data); err != nil {
		return false, fmt.Errorf("failed to unmarshal into target type: %v", err)
	}

	return true, nil
}

func (c *Cache) IsStale(key string, maxAge time.Duration) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	subdirs := []string{"series", "downloads", "state"}
	var filePath string
	var exists bool

	for _, subdir := range subdirs {
		path := filepath.Join(c.BasePath, subdir, key+".json")
		if _, err := os.Stat(path); err == nil {
			filePath = path
			exists = true
			break
		}
	}

	if !exists {
		return true
	}

	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		return true
	}

	var entry CacheEntry
	if err := json.Unmarshal(jsonData, &entry); err != nil {
		return true
	}

	return time.Since(entry.Timestamp) > maxAge
}

func (c *Cache) Clear() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := os.RemoveAll(c.BasePath); err != nil {
		return fmt.Errorf("failed to clear cache: %v", err)
	}

	dirs := []string{"series", "downloads", "state"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(c.BasePath, dir), 0755); err != nil {
			return fmt.Errorf("failed to recreate cache directory %s: %v", dir, err)
		}
	}

	return nil
}

func (c *Cache) List() {
	fmt.Printf("\nCache directory: %s\n", c.BasePath)

	if _, err := os.Stat(c.BasePath); os.IsNotExist(err) {
		fmt.Println("Cache directory does not exist!")
		return
	}

	subdirs := []string{"series", "downloads", "state"}
	for _, subdir := range subdirs {
		path := filepath.Join(c.BasePath, subdir)
		fmt.Printf("\n%s/\n", subdir)

		files, err := os.ReadDir(path)
		if err != nil {
			fmt.Printf("  Error reading directory: %v\n", err)
			continue
		}

		if len(files) == 0 {
			fmt.Printf("  (empty)\n")
			continue
		}

		for _, file := range files {
			if !file.IsDir() {
				info, err := file.Info()
				if err != nil {
					continue
				}
				fmt.Printf("  - %s (%d bytes)\n", file.Name(), info.Size())
			}
		}
	}
	fmt.Println()
}
