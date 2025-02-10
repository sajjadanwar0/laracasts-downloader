package vimeo

import (
	"bufio"
	"os"
	"sync"
)

const (
	// ChunkSize Chunk download settings
	ChunkSize       = 20 * 1024 * 1024 // 20MB chunks
	MaxChunkWorkers = 15               // Concurrent chunks per download
	MaxRetries      = 3                // Maximum retries per chunk
	MemoryBuffer    = 32 * 1024        // 32KB buffer for file operations

)

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
			Dash struct {
				DefaultCDN string `json:"default_cdn"`
				Cdns       map[string]struct {
					URL string `json:"url"`
				} `json:"cdns"`
			} `json:"dash"`
		} `json:"files"`
	} `json:"request"`
}
type BufferedFileWriter struct {
	file    *os.File
	writer  *bufio.Writer
	size    int64
	written int64
	mu      sync.Mutex
}

func NewBufferedFileWriter(path string, size int64) (*BufferedFileWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	// Pre-allocate file
	if err := file.Truncate(size); err != nil {
		err := file.Close()
		if err != nil {
			return nil, err
		}
		return nil, err
	}

	return &BufferedFileWriter{
		file:   file,
		writer: bufio.NewWriterSize(file, MemoryBuffer),
		size:   size,
	}, nil
}

func (w *BufferedFileWriter) WriteAt(p []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Seek(off, 0); err != nil {
		return 0, err
	}

	n, err := w.writer.Write(p)
	if err != nil {
		return n, err
	}

	w.written += int64(n)
	return n, w.writer.Flush()
}

func (w *BufferedFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}
