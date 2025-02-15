# Laracasts  Downloader

A robust Go application that downloads Laracasts topics, series and bits.

## Features

### Topic-Based Organization
- Automatically organizes downloads by topics (e.g., Laravel, Vue, Testing, etc.)
- Creates a clean directory structure for easy navigation
- Generates summary files for each topic and an overall download summary

### Concurrent Downloads
- Uses worker pools for efficient parallel downloading
- Handles multiple topics and series simultaneously
- Implements rate limiting to prevent server overload

### Smart Error Handling
- Retries failed downloads
- Maintains download state to resume interrupted operations
- Creates detailed logs of successes and failures

### Progress Tracking
- Shows real-time download progress
- Provides detailed statistics for each topic
- Creates summary files with download status

## Directory Structure

```
downloads/
├── topics/
│   ├── Laravel/
│   │   ├── Laravel Basics/
│   │   │   └── (series files)
│   │   └── Advanced Laravel/
│   │       └── (series files)
│   ├── Vue/
│   │   └── (series directories)
│   └── Testing/
│       └── (series directories)
├── .cache/
│   │   ├── downloads
│   │   └── series
│   │   └── state

## Usage

1. ```bash
   cp .env.example .env
   ```
2. Set your environment variables in .env

   ```
   EMAIL=your@email.com
   PASSWORD=your_password
   DOWNLOAD_PATH=/path/to/downloads
   VIDEO_QUALITY=1080p  # Options: 360p, 540p, 720p, 1080p
   ```

4. Run the downloader:
```bash
go run main.go
```

The application will:
1. Log in to your Laracasts account
2. Fetch all available topics
3. Create topic directories
4. Download all series for each topic
5. Generate summary files
6. To download series pass the series flag with slug of series no qoutes like `go run main.go -s the-definition-series`

## Configuration

The downloader supports several configuration options:

- **Concurrent Downloads**: Controls how many downloads run in parallel
- **Retry Attempts**: Number of retry attempts for failed downloads
- **Buffer Sizes**: Configurable buffer sizes for optimal performance
- **Video Quality**: Selectable video quality settings (Not fully implemented, future work)

## Dependencies

- Go 1.18 or higher
- ffmpeg (for video processing)

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| EMAIL | Laracasts account email | Yes |
| PASSWORD | Laracasts account password | Yes |
| DOWNLOAD_PATH | Download directory path | Yes |
| VIDEO_QUALITY | Preferred video quality | No (Not fully working atm|

## Performance

The downloader uses several strategies to optimize performance:

- **Buffered Downloads**: Uses buffered I/O for efficient file operations
- **Concurrent Processing**: Parallel processing of topics and series
- **Memory Management**: Efficient memory usage with buffer pools
- **State Management**: Maintains download state for resumability

## Error Handling

The downloader implements comprehensive error handling:

- Retries failed downloads automatically
- Saves error logs for debugging
- Creates detailed download summaries
- Maintains download state for recovery

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create your feature branch
3. Submit a pull request

## License

MIT License
